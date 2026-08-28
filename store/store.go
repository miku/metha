// Package store abstracts metha's on-disk layouts.
//
// A Store is the read side of one harvested (base URL, format, set) triple: it
// knows where the data lives, which files back it, how far the harvest got and
// how to turn the files into records. Commands talk to this interface instead
// of to a directory of files, so that a second layout can be added without
// every command growing a switch.
//
// There are two layouts. In v1 the filename is the entire state: one directory
// per triple, named by the base64 encoding of "set#format#baseURL", holding one
// compressed file per harvested window whose name carries the window's end
// date, which Last reads back to resume a harvest.
//
// In v2 a shard is one base URL, named by a hash so that a cache of every known
// endpoint is not one enormous directory, and the formats and sets harvested
// from it are groups inside it, each with its own run of append-only zstd
// segments. What was harvested is recorded in a sqlite index rather than
// implied by a filename, so a window that returned nothing costs a row instead
// of a file, and a window becomes real in one transaction rather than by a
// rename. The segments are the source of truth; the index is derived and can be
// rebuilt from them.
//
// Iterators yield an error at most once per item. Records stops at the first
// error, since a broken file means the rest of that file cannot be trusted;
// List reports a malformed directory entry and moves on, so that one stray name
// does not hide every other endpoint. Either way the consumer can stop early by
// returning false.
package store

import (
	"errors"
	"fmt"
	"iter"
	"os"
	"slices"

	"github.com/miku/metha"
)

// Layout identifies an on-disk layout version.
type Layout string

// V1 is the original layout: a directory of compressed responses per endpoint.
const V1 Layout = "v1"

// ErrNoBaseURL is returned by Open for an identity without a base URL, which
// would otherwise silently resolve to a directory shared by every such call.
var ErrNoBaseURL = errors.New("store: base url required")

// Identity names a single harvested triple. It is what the CLI takes as an
// endpoint plus -format and -set, and what a store directory encodes.
type Identity struct {
	BaseURL string
	Format  string
	Set     string
}

// String returns the identity in the "set#format#baseURL" form the v1 layout
// encodes into its directory names.
func (id Identity) String() string {
	return id.Set + "#" + id.Format + "#" + id.BaseURL
}

// Entry is one harvested endpoint found under a base directory.
type Entry struct {
	Identity Identity
	Layout   Layout
	Dir      string
}

// DeletedPolicy says what to do with records an endpoint has marked deleted.
// A store keeps them always: a tombstone is the only evidence that a record
// used to exist, and dropping it at write time would lose that for good.
type DeletedPolicy int

const (
	// DeletedKeep yields deleted records along with the rest. It is the zero
	// value because a store hands back what it holds; suppressing tombstones
	// is a decision for whoever is asking.
	DeletedKeep DeletedPolicy = iota
	// DeletedSkip yields only records that are not deleted.
	DeletedSkip
	// DeletedOnly yields only the tombstones.
	DeletedOnly
)

// ReadOptions filters a record stream. The zero value selects everything.
// Bounds are compared against the record datestamp as strings, which works
// because OAI datestamps are ISO 8601: a "2023-01-01" bound also matches the
// "2023-01-01T12:00:00Z" form used by endpoints with finer granularity.
type ReadOptions struct {
	From    string // inclusive lower bound on the record datestamp
	Until   string // inclusive upper bound on the record datestamp
	SetSpec string // only records carrying this setSpec
	Deleted DeletedPolicy
}

// match reports whether a record passes the filter.
func (opts ReadOptions) match(rec *metha.Record) bool {
	if opts.From != "" && rec.Header.DateStamp < opts.From {
		return false
	}
	if opts.Until != "" && rec.Header.DateStamp > opts.Until {
		return false
	}
	if opts.SetSpec != "" && !slices.Contains(rec.Header.SetSpec, opts.SetSpec) {
		return false
	}
	switch opts.Deleted {
	case DeletedSkip:
		return !rec.Deleted()
	case DeletedOnly:
		return rec.Deleted()
	}
	return true
}

// Store is the read side of a single harvested endpoint.
type Store interface {
	// Identity returns the triple this store holds data for.
	Identity() Identity

	// Layout reports the on-disk layout backing this store.
	Layout() Layout

	// Dir is the directory the data lives in. It need not exist yet.
	Dir() string

	// Files lists the data files backing this store, in the order a reader
	// should visit them, and without any temporary files of a harvest in
	// flight. A store that was never harvested has no files and no error.
	Files() ([]string, error)

	// Records streams every stored record matching opts.
	Records(opts ReadOptions) iter.Seq2[metha.Record, error]

	// Last returns the end of the most recently harvested window, in
	// "2006-01-02" form, or the empty string if nothing was harvested yet.
	// This is what a resuming harvest continues from.
	Last() (string, error)
}

// LayoutEnv forces a layout when set, for the cases detection cannot decide:
// where to put a harvest that does not exist yet.
const LayoutEnv = "METHA_LAYOUT"

// Open returns a store for id under baseDir. The layout is detected from the
// cache itself, so a caller never has to know which one it is; a directory
// that does not exist yet reads as the default layout, and opening it is not
// an error - it simply has no files and no records.
func Open(baseDir string, id Identity) (Store, error) {
	return OpenLayout(baseDir, id, Layout(os.Getenv(LayoutEnv)))
}

// OpenLayout is Open with the layout forced. An empty layout means detect.
func OpenLayout(baseDir string, id Identity, layout Layout) (Store, error) {
	if id.BaseURL == "" {
		return nil, ErrNoBaseURL
	}
	if layout == "" {
		layout = Detect(baseDir, id)
	}
	switch layout {
	case V1:
		return &v1Store{baseDir: baseDir, id: id}, nil
	case V2:
		return &v2Store{baseDir: baseDir, id: id}, nil
	default:
		return nil, fmt.Errorf("store: unsupported layout: %s", layout)
	}
}

// Detect reports which layout holds an identity's data. It asks about the
// format and set, not just the endpoint: a half migrated cache can hold oai_dc
// in a shard and marcxml still in a v1 directory, and answering V2 for both
// would make the marcxml data invisible. So a shard that already lists the
// group wins, an existing v1 directory comes next, and only then does the bare
// presence of a shard decide - which is what puts a newly harvested format into
// the shard its endpoint already has. A cache that holds nothing yet reads as
// v1, so the default stays what every existing metha installation has on disk.
func Detect(baseDir string, id Identity) Layout {
	if hasV2Group(baseDir, id) {
		return V2
	}
	if isDir(v1Dir(baseDir, id)) {
		return V1
	}
	if isShard(shardDir(baseDir, id.BaseURL)) {
		return V2
	}
	return V1
}

// hasV2Group reports whether a shard exists for the base URL and already holds
// this format and set. The meta is the shard's own account of its groups, so
// this costs one small file read and never opens the index.
func hasV2Group(baseDir string, id Identity) bool {
	m, err := readMeta(shardDir(baseDir, id.BaseURL))
	if err != nil {
		return false
	}
	return m.hasGroup(id.group())
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Remove deletes everything stored for one identity, which is what a harvest
// asked to start over does. It touches only the given format and set: other
// groups of the same endpoint, and the shard itself, stay.
func Remove(baseDir string, id Identity, layout Layout) error {
	if id.BaseURL == "" {
		return ErrNoBaseURL
	}
	if layout == "" {
		layout = Detect(baseDir, id)
	}
	switch layout {
	case V1:
		return os.RemoveAll(v1Dir(baseDir, id))
	case V2:
		return removeV2(baseDir, id)
	default:
		return fmt.Errorf("store: unsupported layout: %s", layout)
	}
}

// List enumerates the harvested endpoints under baseDir, v1 directories first,
// then v2 shards - a cache can hold both while a migration is under way. A
// base directory that does not exist yields nothing, which is the state of a
// machine that has not harvested anything yet.
func List(baseDir string) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) {
		for entry, err := range listV1(baseDir) {
			if !yield(entry, err) {
				return
			}
		}
		for entry, err := range listV2(baseDir) {
			if !yield(entry, err) {
				return
			}
		}
	}
}
