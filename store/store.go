// Package store abstracts metha's on-disk layouts.
//
// A Store is the read side of one harvested (base URL, format, set) triple: it
// knows where the data lives, which files back it, how far the harvest got and
// how to turn the files into records. Commands talk to this interface instead
// of to a directory of files, so that a second layout can be added without
// every command growing a switch.
//
// Today there is exactly one layout, v1: one directory per triple, named by the
// base64 encoding of "set#format#baseURL", holding one compressed file per
// harvested window, the filename carrying the window's end date. The filename
// is the entire state: Last reads it back to resume a harvest.
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

// ReadOptions filters a record stream. The zero value selects everything.
// Bounds are compared against the record datestamp as strings, which works
// because OAI datestamps are ISO 8601: a "2023-01-01" bound also matches the
// "2023-01-01T12:00:00Z" form used by endpoints with finer granularity.
type ReadOptions struct {
	From  string // inclusive lower bound on the record datestamp
	Until string // inclusive upper bound on the record datestamp
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

// Open returns a store for id under baseDir. The layout is detected from the
// directory itself, so a caller never has to know which one it is; a directory
// that does not exist yet reads as the current default layout, and opening it
// is not an error - it simply has no files and no records.
func Open(baseDir string, id Identity) (Store, error) {
	if id.BaseURL == "" {
		return nil, ErrNoBaseURL
	}
	switch layout := detect(baseDir, id); layout {
	case V1:
		return &v1Store{baseDir: baseDir, id: id}, nil
	default:
		return nil, fmt.Errorf("store: unsupported layout: %s", layout)
	}
}

// detect determines the layout of an already harvested identity. There is only
// one layout so far, so the answer is always V1; the seam is here so that
// adding a layout does not change any caller.
func detect(baseDir string, id Identity) Layout {
	return V1
}

// List enumerates the harvested endpoints under baseDir, in directory order. A
// base directory that does not exist yields nothing, which is the state of a
// machine that has not harvested anything yet.
func List(baseDir string) iter.Seq2[Entry, error] {
	return listV1(baseDir)
}
