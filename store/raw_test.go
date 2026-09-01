package store

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// A segment holds the documents endpoints sent, byte for byte, since responses
// stopped being re-marshalled through oai.Response on the way in. These pin what
// that costs the reader, which is the whole of what the change asked for.

// decompressed reads a whole segment back, which is what "cat *.zst | zstd -dc"
// gets and so what the cache actually holds.
func decompressed(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		t.Fatalf("zstd: %v", err)
	}
	defer dec.Close()
	b, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	return b
}

// appendRaw writes one document into a shard as its own window and returns the
// store to read it back from.
func appendRaw(t *testing.T, docs ...string) (Store, string) {
	t.Helper()
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := w.Begin(from, from.AddDate(0, 1, 0), true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for _, doc := range docs {
		if err := w.Append([]byte(doc)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, base
}

// TestReadsNonUTF8Document: a cache used to be uniformly UTF-8 whatever the
// endpoint sent, because every response was re-marshalled before it was stored.
// Keeping the document that arrived means keeping its encoding too, and a
// decoder without a CharsetReader refuses an honestly declared ISO-8859-1
// document outright - so both the write-side scan and the read have to carry
// one. Without it this shard is unreadable and unindexable, which is the worst
// shape a cache can take: it accepted the bytes and cannot give them back.
func TestReadsNonUTF8Document(t *testing.T) {
	// 0xFC is "ü" in ISO-8859-1, and not valid UTF-8 on its own.
	doc := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>" +
		"<OAI-PMH><ListRecords><record><header><identifier>latin-1</identifier>" +
		"<datestamp>2023-01-05</datestamp></header>" +
		"<metadata><dc><title>Gr\xfcn</title></dc></metadata></record>" +
		"</ListRecords></OAI-PMH>"

	s, _ := appendRaw(t, doc)
	var titles []string
	for rec, err := range s.Records(ReadOptions{}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		titles = append(titles, string(rec.Metadata.Body))
	}
	if len(titles) != 1 {
		t.Fatalf("read %d records, want 1", len(titles))
	}
	// Decoded into UTF-8 on the way out, which is what makes the cache readable
	// by anything downstream whatever the endpoint's encoding was.
	if !strings.Contains(titles[0], "Grün") {
		t.Errorf("record body %q, want it to carry the decoded ü", titles[0])
	}
}

// TestNonUTF8DocumentStreamDoesNotStack: an extent of ISO-8859-1 documents is
// converted once, not once per document.
//
// encoding/xml calls CharsetReader for every declaration it meets and replaces
// its reader with the result, so a converter built over the previous converter
// reads that one's UTF-8 output as ISO-8859-1 and encodes it again. Each
// document adds a layer and each layer doubles every non-ASCII byte, which is
// exponential in the number of documents: the endpoint that found this stored
// 65 of them in 806KB, and the read had spent 600MB and half an hour without
// finishing. The bodies here stay the size they were written, and the ü stays
// one ü rather than becoming Ã¼ and then Ãƒآ¼.
func TestNonUTF8DocumentStreamDoesNotStack(t *testing.T) {
	// 0xFC is "ü" in ISO-8859-1, and not valid UTF-8 on its own.
	doc := func(id string) string {
		return "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>" +
			"<OAI-PMH><ListRecords><record><header><identifier>" + id +
			"</identifier><datestamp>2023-01-05</datestamp></header>" +
			"<metadata><dc><title>Gr\xfcn</title></dc></metadata></record>" +
			"</ListRecords></OAI-PMH>"
	}
	docs := make([]string, 12)
	for i := range docs {
		docs[i] = doc(fmt.Sprint(i))
	}
	s, _ := appendRaw(t, docs...)
	var bodies []string
	for rec, err := range s.Records(ReadOptions{}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		bodies = append(bodies, string(rec.Metadata.Body))
	}
	if len(bodies) != len(docs) {
		t.Fatalf("read %d records, want %d", len(bodies), len(docs))
	}
	// Every record was written identically, so every body has to come back
	// identical too. Under stacking they grow with their position in the extent.
	for i, got := range bodies {
		if got != bodies[0] {
			t.Fatalf("record %d body is %d bytes against %d for the first: the charset reader is stacking",
				i, len(got), len(bodies[0]))
		}
		if !strings.Contains(got, "Grün") {
			t.Fatalf("record %d body %q, want the decoded ü exactly once", i, got)
		}
	}
}

// TestScanCountsNonUTF8Document: the same document has to be countable, or the
// window commits claiming no records and a filtered read skips a shard that
// holds them. The scan is the write side of the same decoder.
func TestScanCountsNonUTF8Document(t *testing.T) {
	doc := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>" +
		"<OAI-PMH><ListRecords><record><header><identifier>latin-1</identifier>" +
		"<datestamp>2023-01-05</datestamp></header>" +
		"<metadata><dc><title>Gr\xfcn</title></dc></metadata></record>" +
		"</ListRecords></OAI-PMH>"

	got, err := scanResponse([]byte(doc))
	if err != nil {
		t.Fatalf("scanResponse: %v", err)
	}
	if got.Records != 1 {
		t.Errorf("counted %d records, want 1", got.Records)
	}
	if got.Lo != "2023-01-05T00:00:00Z" {
		t.Errorf("datestamp bracket %q, want the record's own day", got.Lo)
	}
}

// TestReadsDocumentStream: a segment is documents back to back, each with its
// own XML declaration, since that is how they arrived. A decoder has to walk
// from one to the next rather than stopping at the first, which it only does
// because a declaration is a token it skips rather than a document boundary it
// refuses.
func TestReadsDocumentStream(t *testing.T) {
	doc := func(id string) string {
		return `<?xml version="1.0" encoding="UTF-8"?>` +
			`<OAI-PMH><ListRecords><record><header><identifier>` + id +
			`</identifier><datestamp>2023-01-05</datestamp></header>` +
			`<metadata><dc><title>t</title></dc></metadata></record>` +
			`</ListRecords></OAI-PMH>`
	}
	s, _ := appendRaw(t, doc("one"), doc("two"), doc("three"))
	var ids []string
	for rec, err := range s.Records(ReadOptions{}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids = append(ids, rec.Header.Identifier)
	}
	if want := []string{"one", "two", "three"}; !slices.Equal(ids, want) {
		t.Errorf("read %v, want %v - a stream of documents, not just the first", ids, want)
	}
}

// TestRawKeepsWhatTheModelDrops is the reason for the change. oai.Record models
// a header and a metadata body and nothing else, so an extension element inside
// a header has no field to land in - and while responses were re-marshalled
// before being stored, that meant it never reached the disk either. Now the
// bytes are there whether or not this version of metha can name them.
func TestRawKeepsWhatTheModelDrops(t *testing.T) {
	doc := `<?xml version="1.0"?>` +
		`<OAI-PMH xmlns:custom="http://example.com/x"><ListRecords>` +
		`<record><header><identifier>a</identifier><datestamp>2023-01-05</datestamp>` +
		`<custom:provenance>kept</custom:provenance></header>` +
		`<metadata><dc><title>t</title></dc></metadata></record>` +
		`</ListRecords></OAI-PMH>`

	s, _ := appendRaw(t, doc)
	files, err := s.Files()
	if err != nil || len(files) != 1 {
		t.Fatalf("Files: %v, %v", files, err)
	}
	stored := decompressed(t, files[0])
	if string(stored) != doc {
		t.Errorf("the segment does not hold the document that arrived:\ngot  %s\nwant %s", stored, doc)
	}
	// Named here because it is exactly what the old path lost: an element the
	// reader cannot reach, in the cache anyway.
	if !strings.Contains(string(stored), "custom:provenance") {
		t.Error("an element oai.Record does not model did not reach the disk")
	}
}
