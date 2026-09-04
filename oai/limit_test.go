package oai

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// TestStripBadCharsMatchesReplacer is the safety net on replacing the
// implementation. ControlCharReplacer is what every byte in the cache was
// cleaned with, so the in-place filter has to agree with it exactly - on
// ordinary text, on the characters it removes, on the multi-byte ones, and on
// the boundaries where a three-byte sequence runs off the end of the buffer.
func TestStripBadCharsMatchesReplacer(t *testing.T) {
	cases := []string{
		"",
		"plain ascii",
		"<record><title>ordinary</title></record>",
		"\x00\x01\x02",
		"a\x00b\x01c",
		"keeps \n and \r but not \t",
		"�",
		"￾",
		"a�b￾c",
		"���",
		"unicode survives: äöü 日本語 €",
		// U+FFFF is not in the replacer's set and must be left alone, though it
		// shares the first two bytes with the two that are.
		"￿",
		"a￿b",
		// Truncated multi-byte sequences at the very end, where a three-byte
		// match would read past the buffer.
		"tail\xEF",
		"tail\xEF\xBF",
		"tail\xEF\xBF\xBD",
		// Every byte the table covers, in one string.
		func() string {
			var b strings.Builder
			for c := range 0x20 {
				b.WriteByte(byte(c))
				b.WriteByte('x')
			}
			return b.String()
		}(),
	}
	// Plus random bytes, which is where a hand-written table gets caught out.
	rng := rand.New(rand.NewSource(1))
	for range 500 {
		b := make([]byte, rng.Intn(64))
		for i := range b {
			switch rng.Intn(4) {
			case 0:
				b[i] = byte(rng.Intn(0x20)) // control chars, densely
			case 1:
				b[i] = []byte{0xEF, 0xBF, 0xBD, 0xBE, 0xBC}[rng.Intn(5)]
			default:
				b[i] = byte(rng.Intn(256))
			}
		}
		cases = append(cases, string(b))
	}

	for i, in := range cases {
		want := ControlCharReplacer.Replace(in)
		got := string(stripBadChars([]byte(in)))
		if got != want {
			t.Errorf("case %d: stripBadChars(%q) = %q, replacer gives %q", i, in, got, want)
		}
	}
}

// TestStripBadCharsIsInPlace: the point of the rewrite was to stop holding
// three copies of the body, so the result has to reuse the buffer it was given
// rather than allocate a second one.
func TestStripBadCharsIsInPlace(t *testing.T) {
	b := []byte("a\x00b\x01c")
	out := stripBadChars(b)
	if cap(out) != cap(b) || &out[:1][0] != &b[:1][0] {
		t.Error("stripBadChars allocated a new buffer")
	}
	if string(out) != "abc" {
		t.Errorf("got %q, want abc", out)
	}
}

// TestReadBodyLimit: exactly the limit is fine, one byte more is refused. The
// old form read the limit and kept whatever it got, which cannot tell those two
// apart and so truncated silently.
func TestReadBodyLimit(t *testing.T) {
	for _, tt := range []struct {
		size, limit int
		wantErr     bool
	}{
		{0, 16, false},
		{15, 16, false},
		{16, 16, false},
		{17, 16, true},
		{1 << 20, 16, true},
	} {
		b, err := readBody(strings.NewReader(strings.Repeat("x", tt.size)), tt.limit)
		switch {
		case tt.wantErr && !errors.Is(err, ErrResponseTooLarge):
			t.Errorf("size %d limit %d: err = %v, want ErrResponseTooLarge", tt.size, tt.limit, err)
		case !tt.wantErr && err != nil:
			t.Errorf("size %d limit %d: %v", tt.size, tt.limit, err)
		case !tt.wantErr && len(b) != tt.size:
			t.Errorf("size %d: read %d bytes back", tt.size, len(b))
		}
	}
}

// serve runs a handler and returns its URL.
func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestOversizedBodyIsRefusedNotTruncated: the caller is told the response was
// too big, rather than being handed a fragment or a parse error. An endpoint
// that sends more than metha will read is a different thing from a URL that is
// not an endpoint, and only one of them is worth trying again with a larger
// limit or a smaller window.
func TestOversizedBodyIsRefusedNotTruncated(t *testing.T) {
	const limit = 1 << 16
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords>`)
		fmt.Fprint(w, strings.Repeat(" ", 4*limit))
		fmt.Fprint(w, `</ListRecords></OAI-PMH>`)
	})
	_, err := StdClient.Do(&Request{BaseURL: url, Verb: "ListRecords",
		MetadataPrefix: "oai_dc", MaxBodyBytes: limit})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("err = %v, want ErrResponseTooLarge", err)
	}
	if errors.Is(err, ErrParseFailed) {
		t.Error("reported as a parse failure, which is the confusion the sentinel exists to end")
	}
}

// TestCleanBeforeDecodeIsBounded is the bug this all started from.
//
// The cleaning branch read the body with no limit of its own, and the limit
// that followed it then applied to a reader over bytes already in memory. Since
// a sweep sets CleanBeforeDecode, the bound was absent from the only path that
// does the harvesting.
func TestCleanBeforeDecodeIsBounded(t *testing.T) {
	const limit = 1 << 16
	var served int
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat(" ", 1<<16)
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><OAI-PMH>`)
		for range 64 { // four megabytes, sixty-four times the limit
			fmt.Fprint(w, chunk)
			served += len(chunk)
		}
		fmt.Fprint(w, `</OAI-PMH>`)
	})
	_, err := StdClient.Do(&Request{BaseURL: url, Verb: "ListRecords",
		MetadataPrefix: "oai_dc", MaxBodyBytes: limit, CleanBeforeDecode: true})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("err = %v, want ErrResponseTooLarge", err)
	}
}

// TestCompressionBombIsRefused: the body is sniffed for compression and
// decompressed on the fly, so the bytes on the wire are not the bytes in
// memory. A zstd frame of whitespace runs to about thirty thousand to one, and
// nothing about a response's size on the wire bounds what it costs to read.
func TestCompressionBombIsRefused(t *testing.T) {
	const limit = 1 << 20

	var bomb strings.Builder
	enc, err := zstd.NewWriter(&bomb, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	chunk := make([]byte, 1<<20)
	for i := range chunk {
		chunk[i] = ' '
	}
	const decompressed = 64 << 20 // sixty-four times the limit
	for range decompressed >> 20 {
		if _, err := enc.Write(chunk); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	payload := bomb.String()
	t.Logf("%d bytes on the wire, %d decompressed, ratio %.0fx",
		len(payload), decompressed, float64(decompressed)/float64(len(payload)))
	if len(payload) > limit {
		t.Fatalf("the bomb is %d bytes, which is not under the limit it has to sneak past", len(payload))
	}

	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	})
	// Refused, by whichever layer gets there first: the decoder was given the
	// same bound, so it may reject the frame before the read ever sees it. What
	// must not happen is a success, or sixty-four megabytes being materialised
	// to find out.
	if _, err := StdClient.Do(&Request{BaseURL: url, Verb: "ListRecords",
		MetadataPrefix: "oai_dc", MaxBodyBytes: limit}); err == nil {
		t.Fatal("the bomb was accepted")
	}
}

// TestBodyAtTheLimitStillWorks keeps the bound from being the new bug: a
// perfectly ordinary response has to come back unharmed, cleaned or not.
func TestBodyAtTheLimitStillWorks(t *testing.T) {
	for _, clean := range []bool{false, true} {
		t.Run(fmt.Sprintf("clean=%v", clean), func(t *testing.T) {
			// The control character goes in only for the cleaning case: without
			// cleaning the decoder rejects it, which is the whole reason the
			// switch exists and not something the size bound changed.
			dirt := ""
			if clean {
				dirt = "\x00"
			}
			url := serve(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+
					`<OAI-PMH><ListRecords><record><header>`+
					`<identifier>oai:example.org:1</identifier>`+
					"<datestamp>2023-01-01</datestamp></header>"+dirt+
					`</record></ListRecords></OAI-PMH>`)
			})
			resp, err := StdClient.Do(&Request{BaseURL: url, Verb: "ListRecords",
				MetadataPrefix: "oai_dc", MaxBodyBytes: 1 << 20, CleanBeforeDecode: clean})
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			if n := len(resp.ListRecords.Records); n != 1 {
				t.Fatalf("got %d records, want 1", n)
			}
			if got := resp.ListRecords.Records[0].Header.Identifier; got != "oai:example.org:1" {
				t.Errorf("identifier = %q", got)
			}
			// Raw is what reaches the cache, and the cleaning has to be visible
			// in it: a NUL stored now is a NUL the reader trips over later.
			if clean && strings.ContainsRune(string(resp.Raw), 0) {
				t.Error("a control character survived into Raw")
			}
		})
	}
}

// TestDefaultLimitApplies: a request that says nothing gets the default rather
// than no bound at all, which is what a zero value quietly meant before.
func TestDefaultLimitApplies(t *testing.T) {
	if DefaultMaxBodyBytes <= 0 {
		t.Fatal("DefaultMaxBodyBytes must be positive or the zero value means unbounded")
	}
	if DefaultMaxBodyBytes >= 1<<30 {
		t.Errorf("DefaultMaxBodyBytes = %d, which is the old limit that bounded nothing real",
			DefaultMaxBodyBytes)
	}
}
