package store

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A record that never closes is the one shape that would cost a read its bound:
// the decoder is lenient by design, so an unterminated record does not end where
// its document does - it absorbs every response after it in the extent as its
// own inner XML, and one record comes back worth the whole shard.
//
// The read side used to carry a byte budget around each DecodeElement for this,
// which bought a bound on a state the write side already refuses to create.
// These pin the refusal instead. It is the cheaper guarantee and the stronger
// one: the budget could only cap the damage, and capping it meant abandoning the
// rest of the extent, where rejecting the response keeps the shard whole.
//
// The reason it matters that this is tested at the boundary and not in the
// reader: a reader test has to hand-build the malformed stream, which proves the
// reader's behaviour on input that cannot occur. This proves it cannot occur.

// malformedResponses are documents that would leave a record open, or otherwise
// leave the decoder somewhere it cannot recover from.
func malformedResponses() map[string]string {
	return map[string]string{
		"unclosed record": `<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords>` +
			`<record><header><identifier>cut</identifier></header><metadata><dc>`,
		"cut mid-tag": `<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords>` +
			`<record><header><identifier>cut</identifier></header><metad`,
		"mismatched end tag": `<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords>` +
			`<record><header><identifier>a</identifier><datestamp>2023-01-05</datestamp></header>` +
			`<metadata><dc><title>t</wrong></dc></metadata></record></ListRecords></OAI-PMH>`,
		"stray end tag": `<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords></oops>` +
			`<record><header><identifier>a</identifier><datestamp>2023-01-05</datestamp></header>` +
			`<metadata><dc><title>t</title></dc></metadata></record></ListRecords></OAI-PMH>`,
	}
}

// TestAppendRefusesWhatWouldSwallowTheExtent: every route into a segment goes
// through Append, and Append scans before it writes. A response that will not
// parse is not stored, so no extent holds one, so no read has to survive one.
func TestAppendRefusesWhatWouldSwallowTheExtent(t *testing.T) {
	for name, doc := range malformedResponses() {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
			w, err := OpenWriter(base, id)
			if err != nil {
				t.Fatalf("OpenWriter: %v", err)
			}
			defer func() { _ = w.Close() }()
			from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := w.Begin(from, from.AddDate(0, 1, 0), true); err != nil {
				t.Fatalf("Begin: %v", err)
			}
			if err := w.Append([]byte(doc)); err == nil {
				t.Fatal("Append accepted a response that will not parse; " +
					"an extent can now hold a record that swallows the rest of it")
			}
		})
	}
}

// TestScanResponseRejectsWhatAppendRejects keeps the two in step. Append's
// refusal is scanResponse's refusal, and scanResponse is also what the write
// side counts records with, so a shape that stopped erroring here would start
// being stored without anything else changing.
func TestScanResponseRejectsWhatAppendRejects(t *testing.T) {
	for name, doc := range malformedResponses() {
		if _, err := scanResponse([]byte(doc)); err == nil {
			t.Errorf("%s: scanResponse accepted it, want an error", name)
		}
	}
}

// TestLenientDecoderStillReadsRecoverableSlips is the other half. Refusing
// unbalanced documents must not turn into refusing untidy ones: an end tag that
// closes the wrong open element is recoverable, the record still ends where it
// should, and a cache exists to hold what endpoints actually sent. Here </dc>
// closes <title>, which the lenient decoder allows.
func TestLenientDecoderStillReadsRecoverableSlips(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords>` +
		`<record><header><identifier>untidy</identifier><datestamp>2023-01-05</datestamp></header>` +
		`<metadata><dc><title>t</dc></metadata></record>` +
		`</ListRecords></OAI-PMH>`

	if _, err := scanResponse([]byte(doc)); err != nil {
		t.Fatalf("scanResponse: %v, want it stored: the slip is recoverable", err)
	}
	s, _ := appendRaw(t, doc)
	var got int
	for rec, err := range s.Records(ReadOptions{}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		got++
		// The record ends where its own end tag is, so its body is its own and
		// not the extent's.
		if len(rec.Metadata.Body) > 1<<10 {
			t.Errorf("body is %d bytes for a record of a few dozen", len(rec.Metadata.Body))
		}
	}
	if got != 1 {
		t.Errorf("read %d records, want 1", got)
	}
}

// TestRecordStaysItsOwnSizeAcrossAnExtent: the bound the reader does keep. A
// record is decoded when its element is reached and dropped once yielded, so
// what a read holds is one record however many documents the extent is - which
// is what makes a shard's size the reader's problem and not its memory's.
func TestRecordStaysItsOwnSizeAcrossAnExtent(t *testing.T) {
	doc := func(i int) string {
		return `<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords>` +
			fmt.Sprintf(`<record><header><identifier>id-%d</identifier>`+
				`<datestamp>2023-01-15</datestamp></header>`+
				`<metadata><dc><title>%s</title></dc></metadata></record>`,
				i, strings.Repeat("t", 500)) +
			`</ListRecords></OAI-PMH>`
	}
	docs := make([]string, 200)
	for i := range docs {
		docs[i] = doc(i)
	}
	s, _ := appendRaw(t, docs...)

	var got, largest int
	for rec, err := range s.Records(ReadOptions{}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		got++
		if len(rec.Metadata.Body) > largest {
			largest = len(rec.Metadata.Body)
		}
	}
	if got != len(docs) {
		t.Fatalf("read %d records, want %d", got, len(docs))
	}
	// Every record was written the same size. One that grew with its position
	// would mean the reader is accumulating across documents - which is what a
	// stacked charset converter did, and what newDecoder now prevents.
	if largest > 1<<10 {
		t.Errorf("largest body is %d bytes across %d documents, want each record its own size",
			largest, len(docs))
	}
}
