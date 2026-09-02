package oai

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// countingReader reports how much of a body was actually pulled through it.
type countingReader struct {
	r    io.Reader
	read int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += n
	return n, err
}

// TestMaybeCompressedSniffsWithoutBuffering is a memory regression test.
//
// Detecting zstd or gzip needs four bytes. maybeCompressed used to read the
// whole body to find them, hand back a reader over those bytes, and have
// DoContext read them again - so two full copies of every response were alive
// at once, on every request, compressed or not. At one endpoint nobody
// notices; a sweep at --jobs 256 pays it two hundred and fifty six times over.
//
// The invariant is behavioural rather than a number, which is what makes it
// worth asserting: sniffing must not consume the body.
func TestMaybeCompressedSniffsWithoutBuffering(t *testing.T) {
	body := strings.Repeat("<record>x</record>", 200000) // several megabytes
	c := &countingReader{r: strings.NewReader(body)}

	rc, err := maybeCompressed(c)
	if err != nil {
		t.Fatalf("maybeCompressed: %v", err)
	}
	// Whatever a sniff costs, it is a buffer and not a body.
	if c.read > 1<<16 {
		t.Errorf("sniffing read %d bytes of a %d byte body; it must not buffer the whole thing",
			c.read, len(body))
	}
	// And the body still arrives whole, which is the half that would otherwise
	// be traded away for the saving.
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != body {
		t.Errorf("read back %d bytes, want the %d that went in", len(got), len(body))
	}
}

// TestMaybeCompressedDecompresses: sniffing on a peek must still recognise both
// encodings and hand back the plaintext, since the peeked bytes have to remain
// in front of the decompressor rather than being consumed by the look.
func TestMaybeCompressedDecompresses(t *testing.T) {
	plain := strings.Repeat("<record>hello</record>", 1000)

	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	if _, err := gw.Write([]byte(plain)); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	var zs bytes.Buffer
	zw, err := zstd.NewWriter(&zs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write([]byte(plain)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name string
		body []byte
	}{
		{"gzip", gz.Bytes()},
		{"zstd", zs.Bytes()},
		{"plain", []byte(plain)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rc, err := maybeCompressed(bytes.NewReader(tt.body))
			if err != nil {
				t.Fatalf("maybeCompressed: %v", err)
			}
			got, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(got) != plain {
				t.Errorf("got %d bytes, want the %d that were compressed", len(got), len(plain))
			}
		})
	}
}

// TestMaybeCompressedShortBody: a body too short to hold a magic number is not
// an error. Whatever is wrong with it belongs to the read that follows, and
// answering with an error here would turn a truncated response into a different
// class of failure than a truncated response.
func TestMaybeCompressedShortBody(t *testing.T) {
	for _, body := range []string{"", "<", "<?x"} {
		rc, err := maybeCompressed(strings.NewReader(body))
		if err != nil {
			t.Fatalf("maybeCompressed(%q): %v", body, err)
		}
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read %q: %v", body, err)
		}
		if string(got) != body {
			t.Errorf("read back %q, want %q", got, body)
		}
	}
}
