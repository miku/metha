package store

import (
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// The pre-1.0 layout: one directory per identity, named by the base64 encoding
// of "set#format#baseURL", holding one compressed file per request whose name
// carries the end date of the window it belonged to. The filename was the
// entire state - there was nowhere to record a window that returned nothing, or
// one that was not final yet, which is what the shard layout exists to fix.
//
// Nothing here reads records any more. What is left is what Migrate needs to
// turn a directory into a shard, and what the refusal path needs to tell an
// unmigrated cache from an unharvested one. It is frozen: no harvest has
// written this layout since 1.0, so these names describe a fixed set of bytes
// that can only shrink.

// legacyFilePattern matches a data file written by a pre-1.0 harvest: the
// leading date is the window's end boundary and the serial is the request index
// within that window. Migrate needs the date back, so this is deliberately
// stricter than isLegacyDataFile, which decides what to read.
var legacyFilePattern = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2})-[0-9]{8,}\.xml(\.gz|\.zst)?$`)

// isLegacyDataFile reports whether name is a finalized data file. The extension
// records how the response was compressed, or that it was not, which is what
// -no-compression harvests left behind; temporary files of a harvest that never
// finished carry a "-tmp-<rand>" suffix and so match none of these.
func isLegacyDataFile(name string) bool {
	return strings.HasSuffix(name, ".xml") ||
		strings.HasSuffix(name, ".xml.gz") ||
		strings.HasSuffix(name, ".xml.zst")
}

// isLegacyTempFile reports whether name is the half-written file a pre-1.0
// harvest left when it was interrupted. Responses were written as
// "<date>-<serial>.xml-tmp-<rand>" and renamed into place once complete, so a
// leftover is a truncated response and never data.
//
// It matters only for --rm: this is metha's own litter, so a converted
// directory holding some is still one nothing is lost by removing, where an
// entry metha did not write is not.
func isLegacyTempFile(name string) bool {
	return strings.Contains(name, ".xml") && strings.Contains(name, "-tmp-")
}

// legacyDir returns the directory an identity's pre-1.0 data is in. The name is
// the base64 encoding of the identity, which keeps a directory listing
// reversible; see parseLegacyDir for the other direction.
func legacyDir(baseDir string, id Identity) string {
	return filepath.Join(baseDir, base64.RawURLEncoding.EncodeToString([]byte(id.String())))
}

// LegacyContents is everything an identity's pre-1.0 directory holds, sorted
// into what a migration converts and what it does not. One readdir answers both
// questions a migration asks - what to read, and whether the directory can be
// removed afterwards - so they are answered in one place rather than by two
// walks that could disagree.
type LegacyContents struct {
	Dir     string   // the directory itself
	Data    []string // dated response files, in name order: what Migrate reads
	Undated []string // response files whose name carries no window date
	Temp    []string // half-written files of an interrupted pre-1.0 harvest
	Foreign []string // base names of entries metha did not write, and directories
	Bytes   int64    // what Data occupies on disk, compressed as it lies there
}

// Unconverted names what a migration leaves behind: files it read past, and
// entries that are not its to interpret. An empty result is the whole of the
// case for removing the directory - every byte in it is either in the shard now
// or was never data.
func (c *LegacyContents) Unconverted() []string {
	if len(c.Undated) == 0 && len(c.Foreign) == 0 {
		return nil
	}
	out := make([]string, 0, len(c.Undated)+len(c.Foreign))
	for _, path := range c.Undated {
		out = append(out, filepath.Base(path))
	}
	out = append(out, c.Foreign...)
	return out
}

// ReadLegacyDir sorts the entries of an identity's pre-1.0 directory, reporting
// a directory that is not there as the error it is.
func ReadLegacyDir(baseDir string, id Identity) (*LegacyContents, error) {
	dir := legacyDir(baseDir, id)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	c := &LegacyContents{Dir: dir}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)
		switch {
		case entry.IsDir():
			c.Foreign = append(c.Foreign, name)
		case name == LockName:
			// metha's own, and this process may have just created it.
		case isLegacyDataFile(name):
			if !legacyFilePattern.MatchString(name) {
				c.Undated = append(c.Undated, path)
				continue
			}
			c.Data = append(c.Data, path)
			if info, err := entry.Info(); err == nil {
				c.Bytes += info.Size()
			}
		case isLegacyTempFile(name):
			c.Temp = append(c.Temp, path)
		default:
			c.Foreign = append(c.Foreign, name)
		}
	}
	return c, nil
}

// decompress wraps r in the decompressor its filename calls for.
func decompress(path string, r io.Reader) (io.ReadCloser, error) {
	switch {
	case strings.HasSuffix(path, ".gz"):
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader for %s: %w", path, err)
		}
		return gr, nil
	case strings.HasSuffix(path, ".zst"):
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("failed to create zstd reader for %s: %w", path, err)
		}
		return zr.IOReadCloser(), nil
	default:
		return io.NopCloser(r), nil
	}
}

// ListLegacy enumerates the identities still in the pre-1.0 layout under
// baseDir, which is what "metha migrate" with no endpoint named works through.
// A name that is not an identity is reported as an error and skipped, so that a
// single stray entry cannot hide the rest of the cache.
func ListLegacy(baseDir string) iter.Seq2[Identity, error] {
	return func(yield func(Identity, error) bool) {
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return // Nothing harvested yet.
			}
			yield(Identity{}, err)
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			id, err := parseLegacyDir(entry.Name())
			switch {
			case errors.Is(err, errNotIdentity):
				continue // Some other directory; nothing to report.
			case err != nil:
				if !yield(Identity{}, err) {
					return
				}
				continue
			}
			if !yield(id, nil) {
				return
			}
		}
	}
}

// errNotIdentity marks a directory whose name decodes cleanly but does not
// spell out an identity, so it is simply not ours.
var errNotIdentity = errors.New("not an endpoint directory")

// parseLegacyDir recovers the identity a pre-1.0 directory name encodes.
func parseLegacyDir(name string) (Identity, error) {
	b, err := base64.RawURLEncoding.DecodeString(name)
	if err != nil {
		return Identity{}, fmt.Errorf("%s: %w", name, err)
	}
	parts := strings.SplitN(string(b), "#", 3)
	if len(parts) < 3 {
		return Identity{}, fmt.Errorf("%s: %w", name, errNotIdentity)
	}
	return Identity{Set: parts[0], Format: parts[1], BaseURL: parts[2]}, nil
}

// LegacyRemainder counts the endpoints under baseDir that are still in the
// pre-1.0 layout. It is one readdir of the cache root - a directory name is the
// whole of the evidence - so a listing can afford to ask on every run, which is
// the point: a migration that is half done should say so in a footer rather
// than in an error.
func LegacyRemainder(baseDir string) int {
	var n int
	for _, err := range ListLegacy(baseDir) {
		if err == nil {
			n++
		}
	}
	return n
}

// ErrLegacyLeftover marks a pre-1.0 directory that holds something the
// migration did not convert, so that removing it would lose the only copy.
var ErrLegacyLeftover = errors.New("store: source directory holds files the migration did not convert")

// LegacyLeftoverError names them, in the directory they are in. Which files
// they are is the whole of what an operator needs to act: either they are
// nothing (and the directory goes with an rm -rf), or they are data no window
// date could be read from, and belong somewhere before the source is dropped.
type LegacyLeftoverError struct {
	Dir     string
	Entries []string
}

func (e *LegacyLeftoverError) Error() string {
	names := e.Entries
	suffix := ""
	if len(names) > 3 {
		names, suffix = names[:3], fmt.Sprintf(" and %d more", len(e.Entries)-3)
	}
	return fmt.Sprintf("%v: %s: %s%s", ErrLegacyLeftover, e.Dir, strings.Join(names, ", "), suffix)
}

func (e *LegacyLeftoverError) Unwrap() error { return ErrLegacyLeftover }

// RemoveLegacy deletes one identity's pre-1.0 directory. It is the second step
// of a migration and nothing else calls it, which is why the first check can be
// this blunt: a pre-1.0 directory sits directly in the cache root, so anything
// deeper is not one and must not be removed on its say-so.
//
// What it removes is named rather than swept: the response files a migration
// reads and the half-written ones it steps over, and then the directory itself
// with os.Remove, which refuses one that still holds anything. So an entry
// nobody converted - a file with no window date, a note someone left, a
// subdirectory - stops the removal instead of going with it, and the caller is
// told which. Failing closed is the right direction for the one call in the
// program whose job is to delete the original.
func RemoveLegacy(baseDir string, id Identity) error {
	if id.BaseURL == "" {
		return ErrNoBaseURL
	}
	dir := legacyDir(baseDir, id)
	if filepath.Dir(dir) != filepath.Clean(baseDir) {
		return fmt.Errorf("store: refusing to remove %s: not directly under %s", dir, baseDir)
	}
	c, err := ReadLegacyDir(baseDir, id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // Removed by an earlier run; nothing to do and nothing wrong.
		}
		return err
	}
	if left := c.Unconverted(); len(left) > 0 {
		return &LegacyLeftoverError{Dir: dir, Entries: left}
	}
	for _, path := range slices.Concat(c.Data, c.Temp) {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	// The lock is this process's own, and holding it is over by the time a
	// caller decides to remove; on the platforms without flock there is none.
	if err := os.Remove(filepath.Join(dir, LockName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Remove(dir); err != nil {
		// Something arrived while this ran, which is exactly what the rmdir is
		// here to notice.
		if c, err := ReadLegacyDir(baseDir, id); err == nil {
			if left := c.Unconverted(); len(left) > 0 {
				return &LegacyLeftoverError{Dir: dir, Entries: left}
			}
		}
		return err
	}
	return nil
}

// CheckLegacy reports an identity whose data is still in the pre-1.0 layout. It
// is what a command calls to refuse before doing any work - a harvest in
// particular, which otherwise reaches the network, and its Identify request,
// well before it opens the shard it cannot write.
func CheckLegacy(baseDir string, id Identity) error { return refuseLegacy(baseDir, id) }

// ErrLegacyLayout marks data that is still in the pre-1.0 layout, and so
// unreadable until it is migrated. Errors from Open wrap it.
var ErrLegacyLayout = errors.New("store: data is in the pre-1.0 layout, run migrate")

// LegacyLayoutError names the directory that has to be migrated, and the cache
// it is in. It carries the facts and not the advice: what to type is the
// command line's to say, and it says it once, for every command.
type LegacyLayoutError struct {
	BaseDir string
	Dir     string
}

func (e *LegacyLayoutError) Error() string {
	return fmt.Sprintf("%s: %v", e.Dir, ErrLegacyLayout)
}

func (e *LegacyLayoutError) Unwrap() error { return ErrLegacyLayout }
