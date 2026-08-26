package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha"
)

const (
	// segExt marks a segment file. The extension is honest: a segment is a
	// plain sequence of zstd frames, so every zstd tool can read one.
	segExt = ".zst"
	// segMaxSize is the compressed size past which a new segment is started at
	// the next window boundary. Segments are rotated between windows only, so
	// the frames of one window always land in one file.
	segMaxSize = 256 << 20
)

// frameTarget is how much uncompressed response data accumulates before a frame
// is flushed. One frame per response would compress badly, since a single
// response is small and shares little with itself; one frame per segment would
// rule out reading a record without decompressing everything before it. A
// variable so that tests can produce several frames without writing megabytes.
var frameTarget int64 = 8 << 20

// segFileName returns the name of the nth segment of a group.
func segFileName(n int) string {
	return fmt.Sprintf("%06d%s", n, segExt)
}

// segNumber recovers the sequence number a segment file name encodes.
func segNumber(name string) (int, error) {
	var n int
	_, err := fmt.Sscanf(name, "%06d"+segExt, &n)
	return n, err
}

// frame is one zstd frame inside a segment file.
type frame struct {
	Off int64 // where the frame starts in the segment file
	Len int64 // compressed length of the frame
}

// recordRef locates a record inside the uncompressed content of a frame, and
// carries what an index needs in order to filter without decompressing.
type recordRef struct {
	Identifier string
	Datestamp  string
	Status     string
	SetSpec    string
	Off        int64 // offset within the frame's uncompressed content
	Len        int64
	Sum        string // sha256 of the record bytes, for dedupe
}

// scanRecords finds every record of a raw response and where it sits. The
// offsets are relative to the start of raw; the caller shifts them to the frame
// the response is being appended to.
//
// Byte ranges rather than re-marshaled records, because the blob layer stores
// what the endpoint sent: a range decodes back to exactly the bytes that were
// harvested, which is what makes the index rebuildable and an export
// verifiable.
func scanRecords(raw []byte) ([]recordRef, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false
	var (
		refs  []recordRef
		stack []string
	)
	for {
		// The offset before the token is the end of the previous one, so it
		// can point at whitespace; the record starts at the next '<'.
		prev := dec.InputOffset()
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return refs, nil
		}
		if err != nil {
			return nil, err
		}
		if _, ok := tok.(xml.EndElement); ok {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// Only a record directly under a list or a single record fetch is
		// one. Marshaling a Response emits an empty <GetRecord><record> for
		// the zero value of that field, so every harvested file has a
		// phantom record in it; requiring an identifier drops those without
		// having to know how the response was produced.
		var parent string
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		if se.Name.Local != "record" || (parent != "ListRecords" && parent != "GetRecord") {
			stack = append(stack, se.Name.Local)
			continue
		}
		var rec metha.Record
		// DecodeElement consumes the whole element, end tag included, so the
		// stack is unchanged by a record.
		if err := dec.DecodeElement(&rec, &se); err != nil {
			return nil, err
		}
		if rec.Header.Identifier == "" {
			continue
		}
		end := dec.InputOffset()
		start := prev + int64(bytes.IndexByte(raw[prev:end], '<'))
		sum := sha256.Sum256(raw[start:end])
		// A record can be in several sets. They are kept as one field,
		// space separated, which a setSpec cannot contain: enough for an
		// export to read back, though not for the index to filter on.
		setSpec := strings.Join(rec.Header.SetSpec, " ")
		refs = append(refs, recordRef{
			Identifier: rec.Header.Identifier,
			Datestamp:  rec.Header.DateStamp,
			Status:     rec.Header.Status,
			SetSpec:    setSpec,
			Off:        start,
			Len:        end - start,
			Sum:        hex.EncodeToString(sum[:]),
		})
	}
}

// segWriter appends frames to one segment file. It buffers uncompressed
// responses until a frame is worth writing, and reports where each frame
// landed so the index can point into it.
type segWriter struct {
	path string
	f    *os.File
	enc  *zstd.Encoder
	size int64  // bytes in the file, committed or not
	buf  []byte // responses not yet in a frame
}

// openSegWriter opens a segment for appending, dropping any bytes past the
// length the index vouches for - the tail of a harvest that died mid-window.
func openSegWriter(path string, committed int64, enc *zstd.Encoder) (*segWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Size() > committed {
		if err := f.Truncate(committed); err != nil {
			f.Close()
			return nil, err
		}
	}
	if _, err := f.Seek(committed, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return &segWriter{path: path, f: f, enc: enc, size: committed}, nil
}

// pending returns the number of buffered bytes not yet in a frame.
func (w *segWriter) pending() int64 { return int64(len(w.buf)) }

// append buffers a response and returns its offset within the frame being
// built.
func (w *segWriter) append(raw []byte) int64 {
	off := int64(len(w.buf))
	w.buf = append(w.buf, raw...)
	return off
}

// flush compresses the buffered responses into one frame. It returns the frame
// and whether anything was written.
func (w *segWriter) flush() (frame, bool, error) {
	if len(w.buf) == 0 {
		return frame{}, false, nil
	}
	compressed := w.enc.EncodeAll(w.buf, nil)
	n, err := w.f.Write(compressed)
	if err != nil {
		return frame{}, false, err
	}
	fr := frame{Off: w.size, Len: int64(n)}
	w.size += int64(n)
	w.buf = w.buf[:0]
	return fr, true, nil
}

// truncate cuts the segment back to a length the index vouches for and drops
// whatever was buffered, so that an abandoned window leaves no trace.
func (w *segWriter) truncate(size int64) error {
	w.buf = w.buf[:0]
	if err := w.f.Truncate(size); err != nil {
		return err
	}
	if _, err := w.f.Seek(size, io.SeekStart); err != nil {
		return err
	}
	w.size = size
	return nil
}

// sync forces the appended frames to disk. It has to happen before the index
// records their length, or a crash could leave an index pointing at bytes that
// were never written.
func (w *segWriter) sync() error { return w.f.Sync() }

func (w *segWriter) close() error { return w.f.Close() }

// readFrame decompresses one frame of a segment file.
func readFrame(path string, fr frame) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	compressed := make([]byte, fr.Len)
	if _, err := f.ReadAt(compressed, fr.Off); err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return dec.DecodeAll(compressed, nil)
}
