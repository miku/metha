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

// ErrRecordTooLarge marks an extent abandoned because decoding one record from
// it would not fit in MaxRecordBytes.
//
// It is a different thing from the Oversize callback, and the two are not
// redundant. Oversize is for a record that decoded cleanly and turned out to be
// too big: the record is dropped, the ones after it are read, and nothing is
// lost but the one. This is for a record whose decode cannot be allowed to
// finish, and there is no way back from that - the decoder has consumed an
// unknown amount of the stream, so what follows can no longer be located
// exactly. Everything up to it has already been yielded; the rest of the extent
// is given up on and said so.
//
// What produces it is a response stored truncated. The decoder is deliberately
// lenient, so a record whose closing tag never arrived does not end there: it
// goes on absorbing every response that follows it in the extent as its own
// inner XML, until the bytes run out. That single record is then worth several
// gigabytes, and the size check that used to sit after the decode never got to
// run, because the decode never returned.
var ErrRecordTooLarge = errors.New("store: record too large to decode")

// errRecordBudget is what budgetReader returns, and it never leaves this file:
// recordsFromXML turns it into ErrRecordTooLarge, which says something a caller
// can act on.
var errRecordBudget = errors.New("record budget exhausted")

// budgetReader stops a single decode from reading the world.
//
// It is armed around one DecodeElement and disarmed the rest of the time,
// because a bound on a record is what is wanted and not a bound on the extent -
// which is large on purpose and read whole in the normal case. While armed it
// hands out at most the remaining budget and then refuses, which surfaces as a
// decode error rather than as memory that was already spent.
type budgetReader struct {
	r     io.Reader
	left  int
	armed bool
}

func (b *budgetReader) arm(n int) { b.left, b.armed = n, true }
func (b *budgetReader) disarm()   { b.armed = false }

// recordScanner is a decoder over a stream of response documents together with
// the budget bounding what one record of it may read.
//
// The two travel together because they are only meaningful together: the budget
// has to wrap the reader this decoder reads from, and nothing about a
// *xml.Decoder and a *budgetReader passed side by side says that they do.
// Building both here is what makes the mismatch unrepresentable.
type recordScanner struct {
	xd *xml.Decoder
	// budget is nil when max was not positive, which is how a caller asks for
	// no bound at all - see ReadOptions.MaxRecordBytes.
	budget *budgetReader
}

func newRecordScanner(r io.Reader, max int) *recordScanner {
	if max <= 0 {
		return &recordScanner{xd: newDecoder(r)}
	}
	b := &budgetReader{r: r}
	return &recordScanner{xd: newDecoder(b), budget: b}
}

// record decodes one record from the element just reached, bounded.
func (s *recordScanner) record(rec *oai.Record, start *xml.StartElement, max int) error {
	if s.budget == nil {
		return s.xd.DecodeElement(rec, start)
	}
	// Armed only around this call. Between records the decoder is walking
	// structure and reading almost nothing, and an extent is allowed to be as
	// large as it likes.
	s.budget.arm(recordBudget(max))
	defer s.budget.disarm()
	return s.xd.DecodeElement(rec, start)
}
func (b *budgetReader) Read(p []byte) (int, error) {
	if !b.armed {
		return b.r.Read(p)
	}
	if b.left <= 0 {
		return 0, errRecordBudget
	}
	if len(p) > b.left {
		p = p[:b.left]
	}
	n, err := b.r.Read(p)
	b.left -= n
	return n, err
}

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
	//
	// The budget goes between the decompressor and the XML decoder, which is the
	// only place it can go: what has to be bounded is how much one DecodeElement
	// may pull, and by the time the record is in hand the memory is spent.
	return recordsFromXML(newRecordScanner(dec, opts.MaxRecordBytes), path, opts, yield)
}

// recordBudget is how much one DecodeElement may read for a record bounded at
// max bytes.
//
// Larger than max, and deliberately so: the two bounds catch different things.
// The cheap check after the decode is what handles a record that is merely
// oversized - it is dropped, counted, named, and the extent goes on. The budget
// is the backstop for a record that must not be allowed to finish decoding at
// all, and it costs the rest of the extent, so it should fire only when the
// cheap check could not have. Four times plus a megabyte leaves room for the
// decoder's own buffering and for the difference between a record's inner XML
// and the bytes it was decoded from, without letting anything near the sizes
// that cause trouble.
func recordBudget(max int) int { return 4*max + 1<<20 }

// recordsFromXML yields the matching records of a stream of response documents,
// one record at a time.
//
// A record is decoded when its element is reached and dropped when it has been
// yielded, so what this holds is one record however long the stream is. The
// obvious form - decode a whole oai.Response, walk its ListRecords - holds one
// document instead, and that is a bound only as long as the documents are well
// formed.
//
// They are not. The cache stores what endpoints actually sent, and the decoder
// that reads it back is deliberately lenient (see newDecoder), so a response
// that was cut off mid-flight is stored and read without complaint. Decoding
// whole documents across such a response does not stop at its end - there is no
// end - and the decoder goes on appending records to that one document's
// ListRecords out of every response that follows it in the extent, until the
// bytes run out. The Response is then thrown away for a syntax error, but the
// memory was spent to build it: an export of a large cache was seen reaching
// 125GB and being killed by the OOM killer, on a single decode of a single
// extent whose first document was truncated. Streaming the records makes the
// truncation cost what it should - the error is still reported, at the end,
// with every record before it delivered.
func recordsFromXML(sc *recordScanner, path string, opts ReadOptions, yield func(oai.Record, error) bool) bool {
	// Only <record> directly inside <ListRecords> is a record. The depth check
	// is not pedantry: marcxml metadata has a <record> element of its own, and a
	// bare name match would export the MARC record inside a record as a record.
	// Matching on the local name alone, and not the namespace, is what decoding
	// into oai.Response did, and endpoints that declare no namespace at all are
	// common enough that tightening it here would drop records that have always
	// been exported.
	var stack []string
	for {
		tok, err := sc.xd.Token()
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
			err := sc.record(&rec, &t, opts.MaxRecordBytes)
			if errors.Is(err, errRecordBudget) {
				yield(oai.Record{}, fmt.Errorf("%s: %w", path, ErrRecordTooLarge))
				return false
			}
			if err != nil {
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
