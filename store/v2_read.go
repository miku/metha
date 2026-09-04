package store

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha/oai"
)

// ErrNotHarvested is returned by a read of an identity the cache holds nothing
// for. Answering with silence would read as an endpoint that has no records,
// which is the one answer that is both wrong and plausible.
var ErrNotHarvested = errors.New("store: nothing harvested")

// notHarvested names the identity the cache holds nothing for.
//
// Spelled out rather than formatted with %v: Identity.String is the
// "set#format#baseURL" form the pre-1.0 directory names encode, which is what
// legacyDir needs and not what a person reading an error wants - an unset set
// leaves it opening with a bare "#", and the format sits in the middle of the
// URL it is not part of.
func notHarvested(id Identity) error {
	if id.Set != "" {
		return fmt.Errorf("%s (format %s, set %s): %w", id.BaseURL, id.Format, id.Set, ErrNotHarvested)
	}
	return fmt.Errorf("%s (format %s): %w", id.BaseURL, id.Format, ErrNotHarvested)
}

// Records streams the group's records.
//
// The index is asked first which windows can hold a matching record, and only
// the bytes those windows appended are decompressed: on a shard whose coverage
// spans a decade, a query about last month touches what last month's commits
// wrote instead of all of it.
//
// Going through the index even when nothing is filtered is what makes a read a
// view rather than a dump. A window that was fetched again - because it reached
// into the endpoint's present, or because it had no boundaries at all - leaves
// its older bytes in the segment, since the blob layer is append-only, but the
// extent naming them is gone, and a run of bytes no extent names is one no read
// can reach.
//
// The index prunes; it never decides. Every record that comes out of an extent
// is still checked against the filter, so an index that has fallen behind the
// segments can cost time but cannot produce a wrong answer.
func (s *v2Store) Records(opts ReadOptions) iter.Seq2[oai.Record, error] {
	return func(yield func(oai.Record, error) bool) {
		// Whether the endpoint was harvested at all is meta.json's answer, not
		// the index's: the shard's account of which groups it holds is written
		// when the group is first opened, where the index is written by the
		// first commit. So a harvest that got as far as an empty window reads
		// as empty, and one that was never run reads as never run.
		if !hasGroup(s.baseDir, s.id) {
			yield(oai.Record{}, notHarvested(s.id))
			return
		}
		st, err := loadState(statePath(s.Dir(), s.id.Format, s.id.Set), s.id.Format, s.id.Set)
		if err != nil {
			yield(oai.Record{}, err)
			return
		}
		for _, e := range st.liveExtents(opts) {
			path := filepath.Join(s.segDir(), segFileName(e.Seg))
			if !recordsFromExtent(path, e, opts, yield) {
				return
			}
		}
	}
}

// recordsFromExtent yields the matching records of one run of bytes. It returns
// false when iteration should stop, either because the consumer asked for it or
// because the bytes could not be read, in which case the error was yielded.
//
// The extent is decoded as a stream rather than in one piece: it is a whole
// number of frames, and a month-wide window can hold a great many of them.
//
// The zstd decoder is held to one goroutine. Its default is GOMAXPROCS capped
// at four, and it allocates a window per goroutine, which is a cost paid per
// open reader on a machine that reads as many extents at once as it has cores.
// The parallelism that matters is already at the endpoint level, where export
// put it. See the encoder in v2_writer.go, which is limited for the same reason.
func recordsFromExtent(path string, e extent, opts ReadOptions, yield func(oai.Record, error) bool) bool {
	f, err := os.Open(path)
	if err != nil {
		yield(oai.Record{}, err)
		return false
	}
	defer func() { _ = f.Close() }()
	dec, err := zstd.NewReader(io.NewSectionReader(f, e.Off, e.Len), zstd.WithDecoderConcurrency(1))
	if err != nil {
		yield(oai.Record{}, fmt.Errorf("%s: %w", path, err))
		return false
	}
	defer dec.Close()
	// Each frame holds several responses back to back, and zstd reads a run of
	// frames as one stream, so the whole extent decodes as a sequence of
	// documents.
	return recordsFromXML(newDecoder(dec), path, opts, yield)
}

// recordsFromXML yields the matching records of a stream of response documents,
// one record at a time.
//
// A record is decoded when its element is reached and dropped when it has been
// yielded, so what this holds is one record however long the stream is. The
// obvious form - decode a whole oai.Response, walk its ListRecords - holds one
// document instead, and a document is only as good a bound as it is well formed:
// the decoder is lenient by design (see newDecoder), so it will read a great
// deal of a stream into one element before concluding it was malformed.
//
// Nothing unbalanced enough to provoke that can reach a segment, because Append
// scans every response and refuses the ones that will not parse, so an extent is
// whole documents or nothing. The bound here does not rely on that, which is the
// point of it - and it gets the error handling right as a side effect: a failure
// is reported where it is reached, with every record before it already
// delivered, rather than costing the whole document it appeared in.
func recordsFromXML(dec *xml.Decoder, path string, opts ReadOptions, yield func(oai.Record, error) bool) bool {
	// Only <record> directly inside <ListRecords> is a record. The depth check
	// is not pedantry: marcxml metadata has a <record> element of its own, and a
	// bare name match would export the MARC record inside a record as a record.
	// Matching on the local name alone, and not the namespace, is what decoding
	// into oai.Response did, and endpoints that declare no namespace at all are
	// common enough that tightening it here would drop records that have always
	// been exported.
	var stack []string
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return true
			}
			yield(oai.Record{}, fmt.Errorf("failed to decode XML from %s: %w", path, err))
			return false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "record" || len(stack) == 0 || stack[len(stack)-1] != "ListRecords" {
				stack = append(stack, t.Name.Local)
				continue
			}
			var rec oai.Record
			// Decoding consumes through the matching end element, so the stack
			// stays balanced without pushing this one.
			if err := dec.DecodeElement(&rec, &t); err != nil {
				yield(oai.Record{}, fmt.Errorf("failed to decode XML from %s: %w", path, err))
				return false
			}
			if !opts.match(&rec) {
				continue
			}
			// After the filter, so the count is of records that would have been
			// written, and before the yield, so the body is dropped here rather
			// than going on to cost seven times its size in the renderer.
			if opts.tooLarge(&rec) {
				continue
			}
			if !yield(rec, nil) {
				return false
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
}
