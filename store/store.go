// Package store is metha's on-disk cache.
//
// A Store is the read side of one harvested (base URL, format, set) triple: it
// knows where the data lives, which files back it, how far the harvest got and
// how to turn the files into records. Commands talk to this interface instead
// of to a directory of files.
//
// A shard is one base URL, named by a hash so that a cache of every known
// endpoint is not one enormous directory, and the formats and sets harvested
// from it are groups inside it, each with its own run of append-only zstd
// segments. What was harvested is recorded in a sqlite index rather than
// implied by a filename, so a window that returned nothing costs a row instead
// of a file, and a window becomes real in one transaction rather than by a
// rename. The segments are the source of truth; the index is derived and can be
// rebuilt from them.
//
// The pre-1.0 layout - one directory per triple, named by the base64 encoding
// of "set#format#baseURL", the filename carrying the window it held - is not
// read any more. Opening an identity that is still in it fails with
// ErrLegacyLayout, and Migrate is the whole of what remains: see legacy.go.
//
// Iterators yield an error at most once per item. Records stops at the first
// error, since a broken file means the rest of that file cannot be trusted;
// List reports a malformed directory entry and moves on, so that one stray name
// does not hide every other endpoint. Either way the consumer can stop early by
// returning false.
package store

import (
	"errors"
	"iter"
	"os"
	"slices"

	"github.com/miku/metha"
)

// ErrNoBaseURL is returned by Open for an identity without a base URL, which
// would otherwise silently resolve to a directory shared by every such call.
var ErrNoBaseURL = errors.New("store: base url required")

// Identity names a single harvested triple. It is what the CLI takes as an
// endpoint plus --format and --set.
type Identity struct {
	BaseURL string
	Format  string
	Set     string
}

// String returns the identity in the "set#format#baseURL" form the pre-1.0
// layout encoded into its directory names, which is the one thing still read
// that way - see legacyDir.
func (id Identity) String() string {
	return id.Set + "#" + id.Format + "#" + id.BaseURL
}

// Entry is one harvested endpoint found under a base directory.
type Entry struct {
	Identity Identity
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
//
// Bounds and datestamps are ISO 8601, and both come in either of the two
// granularities OAI-PMH allows: "2023-01-01" and "2023-01-01T12:00:00Z". They
// are widened to the second before being compared, because as text the two
// forms do not line up - see widen.
type ReadOptions struct {
	From    string // inclusive lower bound on the record datestamp
	Until   string // inclusive upper bound on the record datestamp
	SetSpec string // only records carrying this setSpec
	Deleted DeletedPolicy
}

// match reports whether a record passes the filter.
func (opts ReadOptions) match(rec *metha.Record) bool {
	if opts.From != "" && widen(rec.Header.DateStamp, dayStart) < widen(opts.From, dayStart) {
		return false
	}
	if opts.Until != "" && widen(rec.Header.DateStamp, dayStart) > widen(opts.Until, dayEnd) {
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

// The two shapes an OAI-PMH datestamp is allowed to take, and the times of day
// a date-only one stands for at each end of a range.
const (
	dayLen   = len("2006-01-02")
	dayStart = "T00:00:00Z"
	dayEnd   = "T23:59:59Z"
)

// widen pads a date-only datestamp out to a full second, tail supplying the
// time of day. It is what lets the two granularities be compared as text at
// all: "2023-05-01T14:23:00Z" sorts after "2023-05-01", because it is longer
// and shares its prefix, so an -until of a bare date used to drop every
// second-granularity record of the day it named - including the one stamped at
// midnight, which is the same instant as the bound.
//
// Widening the bound rather than truncating the datestamp is also what the
// protocol means by a date-only bound: the start of that day at the lower end,
// the end of it at the upper one. Pass dayStart for a datestamp, which stands
// for the day it names either way.
//
// Anything that is not a bare date is returned untouched. An endpoint that
// stamps records in some third form is then compared exactly as before, rather
// than pushed into a shape it does not have.
func widen(stamp, tail string) string {
	if len(stamp) != dayLen || stamp[4] != '-' || stamp[7] != '-' {
		return stamp
	}
	return stamp + tail
}

// Store is the read side of a single harvested endpoint.
type Store interface {
	// Identity returns the triple this store holds data for.
	Identity() Identity

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

// Open returns a store for id under baseDir. A shard that does not exist yet is
// not an error - it simply has no files and no records, which is what a harvest
// about to create it looks like. An identity whose data is still in the pre-1.0
// layout is refused: see refuseLegacy.
func Open(baseDir string, id Identity) (Store, error) {
	if id.BaseURL == "" {
		return nil, ErrNoBaseURL
	}
	if err := refuseLegacy(baseDir, id); err != nil {
		return nil, err
	}
	return &v2Store{baseDir: baseDir, id: id}, nil
}

// refuseLegacy fails an identity whose data is still in a pre-1.0 directory,
// rather than answering for the empty shard beside it. Silence there would read
// as an endpoint that was never harvested, which is the one answer that is both
// wrong and plausible.
//
// The question is asked about the format and set, not just the endpoint: a half
// migrated cache can hold oai_dc in a shard and marcxml still in a directory,
// and the shard is the answer only for the groups it actually lists.
func refuseLegacy(baseDir string, id Identity) error {
	dir := legacyDir(baseDir, id)
	if !isDir(dir) || hasGroup(baseDir, id) {
		return nil
	}
	return &LegacyLayoutError{BaseDir: baseDir, Dir: dir}
}

// hasGroup reports whether a shard exists for the base URL and already holds
// this format and set. The meta is the shard's own account of its groups, so
// this costs one small file read and never opens the index.
func hasGroup(baseDir string, id Identity) bool {
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
func Remove(baseDir string, id Identity) error {
	if id.BaseURL == "" {
		return ErrNoBaseURL
	}
	return removeV2(baseDir, id)
}

// List enumerates the harvested endpoints under baseDir, one entry per group. A
// base directory that does not exist yields nothing, which is the state of a
// machine that has not harvested anything yet. Endpoints still in the pre-1.0
// layout are not listed; LegacyRemainder counts them.
func List(baseDir string) iter.Seq2[Entry, error] {
	return listV2(baseDir)
}
