package store

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/miku/metha/oai"
)

// mojibake applies the UTF-8/CP1252 double-encoding loop n times to s.
//
// It is the shape the bound exists for. A repository that re-encodes its own
// text on every reindex grows it by about 2.2x a round once every byte is above
// 0x7f, so a single character becomes a megabyte by the sixteenth pass and
// tens of megabytes by the twenty-first, which is how 1326 records came to hold
// one of 1.6GB.
func mojibake(s string, n int) string {
	for range n {
		var b strings.Builder
		for _, c := range []byte(s) {
			b.WriteRune(cp1252[c])
		}
		s = b.String()
	}
	return s
}

// cp1252 maps a byte to the rune Windows-1252 gives it. Only the high half
// differs from Latin-1, and only the eight undefined positions are left as the
// replacement they decode to here.
var cp1252 = func() [256]rune {
	var t [256]rune
	for i := range t {
		t[i] = rune(i)
	}
	for i, r := range []rune{
		'€', '�', '‚', 'ƒ', '„', '…', '†', '‡',
		'ˆ', '‰', 'Š', '‹', 'Œ', '�', 'Ž', '�',
		'�', '‘', '’', '“', '”', '•', '–', '—',
		'˜', '™', 'š', '›', 'œ', '�', 'ž', 'Ÿ',
	} {
		t[0x80+i] = r
	}
	return t
}()

// TestMojibakeGrows is the premise, checked rather than assumed: the growth is
// geometric and converges on 2.2x a round, so no bound derived from what the
// harvest stored can bound what the read produces.
func TestMojibakeGrows(t *testing.T) {
	s, prev := "€", 0
	for i := 1; i <= 12; i++ {
		s = mojibake(s, 1)
		n := len(s)
		if i > 6 {
			if ratio := float64(n) / float64(prev); ratio < 2.1 || ratio > 2.3 {
				t.Errorf("layer %d: grew %.3fx, want about 2.2x", i, ratio)
			}
		}
		prev = n
	}
	if prev < 1<<15 {
		t.Errorf("twelve layers of one character came to %d bytes, want tens of thousands", prev)
	}
}

// shardWithRecords writes one window holding the given records and opens it.
func shardWithRecords(t *testing.T, recs ...oai.Record) Store {
	t.Helper()
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if err := w.Begin(day(t, "2023-01-01"), day(t, "2023-01-31"), true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.Append(marshal(t, oai.Response{ListRecords: oai.ListRecords{Records: recs}})); err != nil {
		t.Fatalf("Append: %v", err)
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
	return s
}

func recordOfSize(id string, n int) oai.Record {
	return oai.Record{
		Header:   oai.Header{Identifier: id, DateStamp: "2023-01-15"},
		Metadata: oai.Metadata{Body: []byte("<dc:title>" + strings.Repeat("x", n) + "</dc:title>")},
	}
}

// TestMaxRecordBytesDropsAndReports: the oversized record is left out, the ones
// around it are not, and the caller is told which identifier went and how big it
// was. Being told is the point - a corpus dump that silently loses records is
// worse than one that fails.
func TestMaxRecordBytesDropsAndReports(t *testing.T) {
	s := shardWithRecords(t,
		recordOfSize("small-before", 10),
		recordOfSize("huge", 200<<10),
		recordOfSize("small-after", 10),
	)

	var (
		got      []string
		reported []string
	)
	opts := ReadOptions{
		MaxRecordBytes: 64 << 10,
		Oversize: func(id string, n int) {
			reported = append(reported, fmt.Sprintf("%s:%d", id, n))
		},
	}
	for rec, err := range s.Records(opts) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		got = append(got, rec.Header.Identifier)
	}

	want := []string{"small-before", "small-after"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
	if len(reported) != 1 || !strings.HasPrefix(reported[0], "huge:") {
		t.Fatalf("Oversize reported %v, want one entry for huge", reported)
	}
	// The size handed to the callback is the record's, not the limit's.
	if n := len(recordOfSize("huge", 200<<10).Metadata.Body); reported[0] != fmt.Sprintf("huge:%d", n) {
		t.Errorf("Oversize said %q, want huge:%d", reported[0], n)
	}
}

// TestMaxRecordBytesZeroIsUnbounded keeps the default behaviour honest: the
// zero value of ReadOptions selects everything, and that has to keep meaning
// everything.
func TestMaxRecordBytesZeroIsUnbounded(t *testing.T) {
	s := shardWithRecords(t, recordOfSize("huge", 200<<10))
	var n int
	for rec, err := range s.Records(ReadOptions{}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		if len(rec.Metadata.Body) < 200<<10 {
			t.Errorf("record came back short: %d bytes", len(rec.Metadata.Body))
		}
		n++
	}
	if n != 1 {
		t.Errorf("got %d records, want 1", n)
	}
}

// TestMaxRecordBytesRunsAfterTheFilter: a record excluded by a datestamp bound
// was never going to be written, so it must not be reported as dropped. The
// count is of records the run actually lost.
func TestMaxRecordBytesRunsAfterTheFilter(t *testing.T) {
	huge := recordOfSize("huge", 200<<10)
	huge.Header.DateStamp = "2023-01-15"
	s := shardWithRecords(t, huge)

	var reported int
	opts := ReadOptions{
		From:           "2024-01-01", // excludes it on the datestamp
		MaxRecordBytes: 1 << 10,
		Oversize:       func(string, int) { reported++ },
	}
	for _, err := range s.Records(opts) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
	}
	if reported != 0 {
		t.Errorf("reported %d oversized records, want 0: the filter had already excluded it", reported)
	}
}

// TestRenderPassesTheBoundThrough: the option has to survive the trip from
// RenderOpts into ReadOptions, which is the only path export uses.
func TestRenderPassesTheBoundThrough(t *testing.T) {
	s := shardWithRecords(t, recordOfSize("small", 10), recordOfSize("huge", 200<<10))
	var (
		sb       strings.Builder
		reported int
	)
	err := Render(s, RenderOpts{
		Writer:         &sb,
		UseJson:        true,
		MaxRecordBytes: 64 << 10,
		Oversize:       func(string, int) { reported++ },
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if reported != 1 {
		t.Errorf("reported %d, want 1", reported)
	}
	if n := strings.Count(strings.TrimSpace(sb.String()), "\n") + 1; n != 1 {
		t.Errorf("wrote %d lines, want 1", n)
	}
	if strings.Contains(sb.String(), "huge") {
		t.Error("the oversized record was written anyway")
	}
}

// TestOversizeRecordIsRealisticallyShaped runs the bound against what actually
// arrived rather than against a run of x's: a record whose metadata is one
// enormous mojibake text node, which is the case with no internal structure for
// the size of the body to be inferred from.
func TestOversizeRecordIsRealisticallyShaped(t *testing.T) {
	body := "<oai_dc:dc><dc:source>Revista Conscientia; " + mojibake("I Simpósio", 17) + "</dc:source></oai_dc:dc>"
	if len(body) < 1<<20 {
		t.Fatalf("the fixture came to %d bytes, want over a megabyte", len(body))
	}
	rec := oai.Record{
		Header:   oai.Header{Identifier: "oai:ojs.pkp.sfu.ca:article/670", DateStamp: "2023-01-15"},
		Metadata: oai.Metadata{Body: []byte(body)},
	}
	s := shardWithRecords(t, rec)

	var reported []int
	err := Render(s, RenderOpts{
		Writer:         io.Discard,
		UseJson:        true,
		MaxRecordBytes: 1 << 20,
		Oversize:       func(_ string, n int) { reported = append(reported, n) },
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(reported) != 1 {
		t.Fatalf("reported %v, want one drop", reported)
	}
	if reported[0] <= 1<<20 {
		t.Errorf("reported %d bytes, want more than the limit", reported[0])
	}
}

// TestLargeBodySkipsTheMap is the second half of the bound, and it is what
// protects the paths that have no limit set. Past maxMapXML a body is emitted
// as a JSON string of itself rather than built into a map, which is a change of
// shape - an object becomes a string - and the reason to accept it is that the
// map costs about seven times the body and this costs one.
func TestLargeBodySkipsTheMap(t *testing.T) {
	for _, tt := range []struct {
		name       string
		size       int
		wantObject bool
	}{
		{"a normal record is still an object", 1 << 10, true},
		{"just under the threshold", 4<<20 - 4096, true},
		{"over the threshold", 8 << 20, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte("<dc><title>" + strings.Repeat("x", tt.size) + "</title></dc>")
			b, err := oai.Metadata{Body: body}.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON: %v", err)
			}
			// Whichever branch it took, the result has to be valid JSON: this
			// runs inside json.Marshal of the whole record, which fails the
			// entire line if a field's marshaller returns something malformed.
			if !json.Valid(b) {
				t.Fatal("produced invalid JSON")
			}
			if gotObject := b[0] == '{'; gotObject != tt.wantObject {
				t.Errorf("marshalled to %c..., wantObject=%v", b[0], tt.wantObject)
			}
			// Either way the text itself survives; only its framing changed.
			if !strings.Contains(string(b), strings.Repeat("x", 64)) {
				t.Error("the body did not survive")
			}
		})
	}
}

// TestLargeBodyCostsLess is the claim the threshold rests on, measured rather
// than asserted: going through the map costs several times what emitting the
// body as a string does.
func TestLargeBodyCostsLess(t *testing.T) {
	body := []byte("<dc><title>" + strings.Repeat("x", 8<<20) + "</title></dc>")
	viaString := allocs(t, func() {
		if _, err := (oai.Metadata{Body: body}).MarshalJSON(); err != nil {
			t.Fatal(err)
		}
	})
	small := []byte("<dc><title>" + strings.Repeat("x", 1<<20) + "</title></dc>")
	viaMap := allocs(t, func() {
		if _, err := (oai.Metadata{Body: small}).MarshalJSON(); err != nil {
			t.Fatal(err)
		}
	})
	perByteString := float64(viaString) / float64(len(body))
	perByteMap := float64(viaMap) / float64(len(small))
	t.Logf("map path %.1fx the body, string path %.1fx", perByteMap, perByteString)
	if perByteString >= perByteMap {
		t.Errorf("the string path cost %.1fx and the map path %.1fx; the threshold buys nothing",
			perByteString, perByteMap)
	}
}

// allocs reports the bytes f allocated.
func allocs(t *testing.T, f func()) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}
