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

// legacyDir returns the directory an identity's pre-1.0 data is in. The name is
// the base64 encoding of the identity, which keeps a directory listing
// reversible; see parseLegacyDir for the other direction.
func legacyDir(baseDir string, id Identity) string {
	return filepath.Join(baseDir, base64.RawURLEncoding.EncodeToString([]byte(id.String())))
}

// legacyFiles lists an identity's pre-1.0 data files, reporting a directory
// that is not there as the error it is.
func legacyFiles(baseDir string, id Identity) ([]string, error) {
	dir := legacyDir(baseDir, id)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if isLegacyDataFile(entry.Name()) {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files, nil
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

// RemoveLegacy deletes one identity's pre-1.0 directory. It is the second step
// of a migration and nothing else calls it, which is why the check below can be
// this blunt: a pre-1.0 directory sits directly in the cache root, so anything
// deeper is not one and must not be removed on its say-so.
func RemoveLegacy(baseDir string, id Identity) error {
	if id.BaseURL == "" {
		return ErrNoBaseURL
	}
	dir := legacyDir(baseDir, id)
	if filepath.Dir(dir) != filepath.Clean(baseDir) {
		return fmt.Errorf("store: refusing to remove %s: not directly under %s", dir, baseDir)
	}
	return os.RemoveAll(dir)
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
