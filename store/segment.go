package store

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha/oai"
	"golang.org/x/net/html/charset"
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

// newDecoder reads XML the way everything in the cache has to be read.
//
// Lenient, because a cache holds what endpoints actually sent and refusing to
// read it back would make the cache useless for exactly the responses it is
// most worth having kept. And with a charset reader, because a segment holds
// the documents as they arrived: since responses stopped being re-marshalled
// through oai.Response, a cache is no longer uniformly UTF-8, and an
// ISO-8859-1 document with an honest declaration is one a decoder without this
// refuses outright.
func newDecoder(r io.Reader) *xml.Decoder {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	dec.CharsetReader = charset.NewReaderLabel
	return dec
}

// scanned is what one raw response contributes to the window it lands in: how
// many records it carried, how many of those were tombstones, and the range of
// datestamps they fall in.
type scanned struct {
	Records int
	Deleted int
	Lo, Hi  string // widened to whole seconds; empty when they cannot be bracketed
}

// scanResponse counts the records of a raw response and brackets their
// datestamps. It is the only reason a write parses what it stores at all: the
// counts are what a migration verifies against and what metha stat reports, and
// the bracket is what lets a filtered read skip a window whole.
//
// It deliberately learns nothing else. Addressing individual records inside a
// window was what the record index bought, and a window is the unit of
// atomicity everywhere else, so it is the unit of addressing too.
func scanResponse(raw []byte) (scanned, error) {
	dec := newDecoder(bytes.NewReader(raw))
	var (
		out   scanned
		stack []string
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return scanned{}, err
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
		var rec oai.Record
		// DecodeElement consumes the whole element, end tag included, so the
		// stack is unchanged by a record.
		if err := dec.DecodeElement(&rec, &se); err != nil {
			return scanned{}, err
		}
		if rec.Header.Identifier == "" {
			continue
		}
		out.Records++
		if rec.Deleted() {
			out.Deleted++
		}
		out.cover(stampBounds(rec.Header.DateStamp))
	}
}

// cover grows the datestamp bracket to hold another record's. It is called once
// per record, straight after the count, so a Records of one is the first. A pair
// that could not be bracketed - a datestamp in some shape no bound can be
// ordered against - makes the whole scan unbounded, and an unbounded window is
// one a filtered read never skips.
func (s *scanned) cover(lo, hi string) {
	switch {
	case s.Records == 1:
		s.Lo, s.Hi = lo, hi
	case lo == "" || s.Lo == "":
		s.Lo, s.Hi = "", ""
	default:
		s.Lo, s.Hi = min(s.Lo, lo), max(s.Hi, hi)
	}
}

// segWriter appends frames to one segment file. It buffers uncompressed
// responses until a frame is worth writing.
//
// Frames are still how a segment is written - one frame per response would
// compress badly and one per segment would rule out reading anything without
// decompressing everything before it - but nothing addresses one any more. A
// window's frames are contiguous, because appends happen only inside a window
// and segments rotate only between them, so the run of bytes a commit appended
// is a valid zstd stream on its own.
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
		_ = f.Close()
		return nil, err
	}
	if info.Size() > committed {
		if err := f.Truncate(committed); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	if _, err := f.Seek(committed, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &segWriter{path: path, f: f, enc: enc, size: committed}, nil
}

// pending returns the number of buffered bytes not yet in a frame.
func (w *segWriter) pending() int64 { return int64(len(w.buf)) }

// append buffers a response.
func (w *segWriter) append(raw []byte) { w.buf = append(w.buf, raw...) }

// flush compresses the buffered responses into one frame.
func (w *segWriter) flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	n, err := w.f.Write(w.enc.EncodeAll(w.buf, nil))
	if err != nil {
		return err
	}
	w.size += int64(n)
	w.buf = w.buf[:0]
	return nil
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

// discardIfEmpty removes the file when it ends up holding nothing. A segment is
// created by the first append, so an empty one means every window that appended
// to it was aborted - a harvest interrupted or failed before its first commit -
// and a zero-length file is not a segment, it is the shape of one. Leaving it
// would also keep the group directory from being tidied away, which is the
// difference between an endpoint that reads as never harvested and one that
// reads as harvested and empty.
//
// Called after close, so the descriptor is gone and only the name is being
// removed. Errors are ignored: the file is already unreachable through the
// index, so failing to unlink it is untidy rather than wrong.
func (w *segWriter) discardIfEmpty() {
	if w.size == 0 {
		_ = os.Remove(w.path)
	}
}
