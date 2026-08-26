package store

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/xml"
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
	"github.com/miku/metha"
)

// v1FilePattern matches a v1 filename written by a harvest: the leading date is
// the window's end boundary and the serial is the request index within that
// window. Only Last needs the date back, so this is deliberately stricter than
// isV1DataFile, which decides what to read. harvest.go writes these names and
// keeps its own copy of the pattern to resume from; v1 is frozen, so the two
// cannot drift.
var v1FilePattern = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2})-[0-9]{8,}\.xml(\.gz|\.zst)?$`)

// isV1DataFile reports whether name is a finalized data file. The extension
// records how the response was compressed, or that it was not, which is what
// -no-compression harvests leave behind; temporary files of a harvest in flight
// carry a "-tmp-<rand>" suffix and so match none of these.
func isV1DataFile(name string) bool {
	return strings.HasSuffix(name, ".xml") ||
		strings.HasSuffix(name, ".xml.gz") ||
		strings.HasSuffix(name, ".xml.zst")
}

// v1Store reads the original layout: one directory per identity, one file per
// request, the filename carrying the harvested window's end date.
type v1Store struct {
	baseDir string
	id      Identity
}

// v1Dir returns the directory an identity is stored in. The name is the base64
// encoding of the identity, which keeps a directory listing reversible; see
// listV1 for the other direction.
func v1Dir(baseDir string, id Identity) string {
	return filepath.Join(baseDir, base64.RawURLEncoding.EncodeToString([]byte(id.String())))
}

func (s *v1Store) Identity() Identity { return s.id }

func (s *v1Store) Layout() Layout { return V1 }

func (s *v1Store) Dir() string { return v1Dir(s.baseDir, s.id) }

// Files returns the data files in datestamp order, which is what makes
// "metha-files ... | xargs cat" read chronologically. An endpoint that was
// never harvested has no files and no error.
func (s *v1Store) Files() ([]string, error) {
	files, err := s.dataFiles()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return files, err
}

// dataFiles is Files, but reports a missing directory as the error it is, which
// is how reading records from a never harvested endpoint is caught.
func (s *v1Store) dataFiles() ([]string, error) {
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if isV1DataFile(entry.Name()) {
			files = append(files, filepath.Join(s.Dir(), entry.Name()))
		}
	}
	return files, nil
}

func (s *v1Store) Records(opts ReadOptions) iter.Seq2[metha.Record, error] {
	return func(yield func(metha.Record, error) bool) {
		files, err := s.dataFiles()
		if err != nil {
			yield(metha.Record{}, err)
			return
		}
		for _, file := range files {
			// The filename starts with the window's end date, so a file
			// named before the lower bound cannot hold a matching record.
			if opts.From != "" && filepath.Base(file) < opts.From {
				continue
			}
			if !recordsFromFile(file, opts, yield) {
				return
			}
		}
	}
}

// recordsFromFile yields the matching records of a single file. It returns
// false when iteration should stop, either because the consumer asked for it or
// because the file could not be read, in which case the error was yielded.
func recordsFromFile(path string, opts ReadOptions, yield func(metha.Record, error) bool) bool {
	f, err := os.Open(path)
	if err != nil {
		yield(metha.Record{}, err)
		return false
	}
	defer f.Close()
	r, err := decompress(path, f)
	if err != nil {
		yield(metha.Record{}, err)
		return false
	}
	defer r.Close()
	// A single file can hold more than one response: metha-pack concatenates
	// compressed frames into one file, and both readers stream those members
	// transparently.
	dec := xml.NewDecoder(r)
	dec.Strict = false
	for {
		var resp metha.Response
		if err := dec.Decode(&resp); err != nil {
			if errors.Is(err, io.EOF) {
				return true
			}
			yield(metha.Record{}, fmt.Errorf("failed to decode XML from %s: %w", path, err))
			return false
		}
		for _, rec := range resp.ListRecords.Records {
			if opts.From != "" && rec.Header.DateStamp < opts.From {
				continue
			}
			if opts.Until != "" && rec.Header.DateStamp > opts.Until {
				continue
			}
			if !yield(rec, nil) {
				return false
			}
		}
	}
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

// Last returns the largest window end date over the stored filenames. That
// readdir is the whole of the v1 resume state.
func (s *v1Store) Last() (string, error) {
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var last string
	for _, entry := range entries {
		groups := v1FilePattern.FindStringSubmatch(entry.Name())
		if len(groups) > 1 && groups[1] > last {
			last = groups[1]
		}
	}
	return last, nil
}

// listV1 enumerates the endpoint directories under baseDir. A name that is not
// a v1 identity is reported as an error and skipped, so that a single stray
// entry cannot hide the rest of the cache.
func listV1(baseDir string) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) {
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return // Nothing harvested yet.
			}
			yield(Entry{}, err)
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			id, err := parseV1Dir(entry.Name())
			switch {
			case errors.Is(err, errNotIdentity):
				continue // Some other directory; nothing to report.
			case err != nil:
				if !yield(Entry{}, err) {
					return
				}
				continue
			}
			e := Entry{
				Identity: id,
				Layout:   V1,
				Dir:      filepath.Join(baseDir, entry.Name()),
			}
			if !yield(e, nil) {
				return
			}
		}
	}
}

// errNotIdentity marks a directory whose name decodes cleanly but does not
// spell out an identity, so it is simply not ours.
var errNotIdentity = errors.New("not an endpoint directory")

// parseV1Dir recovers the identity a v1 directory name encodes.
func parseV1Dir(name string) (Identity, error) {
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
