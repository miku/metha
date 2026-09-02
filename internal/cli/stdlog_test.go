package cli

import (
	"bufio"
	"bytes"
	"io"
	stdlog "log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNoiseFilter(t *testing.T) {
	var cases = []struct {
		name string
		line string
		want string
	}{
		{
			name: "noise is dropped",
			line: `2026/09/02 09:08:00 Unsolicited response received on idle HTTP channel starting with "<6w\bz"; err=<nil>`,
			want: "",
		},
		{
			name: "anything else passes through",
			line: "2026/09/02 09:08:00 something worth reading",
			want: "2026/09/02 09:08:00 something worth reading",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			f := noiseFilter{w: &buf}
			n, err := f.Write([]byte(c.line))
			if err != nil {
				t.Fatalf("write: %v", err)
			}
			if n != len(c.line) {
				t.Errorf("got n=%d, want %d: a filtered write must still look complete to log.Logger", n, len(c.line))
			}
			if got := buf.String(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// syncBuffer is a bytes.Buffer written from net/http's read loop and read from
// the test's goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestRouteStdlibLogUnsolicited pins the fix to the message net/http actually
// writes. The string in httpNoise is copied out of the standard library and
// nothing enforces it, so this drives a real transport into the real code path:
// a server that answers a request and then writes garbage onto the connection
// once it is idle in the pool.
func TestRouteStdlibLogUnsolicited(t *testing.T) {
	t.Cleanup(func() { stdlog.SetOutput(os.Stderr) })

	// Unfiltered first, so a version of net/http that stopped logging this, or
	// a test that stopped provoking it, fails here rather than passing quietly
	// below.
	var plain syncBuffer
	stdlog.SetOutput(&plain)
	provokeUnsolicited(t)
	if got := plain.String(); !strings.Contains(got, "Unsolicited response received on idle HTTP channel") {
		t.Fatalf("net/http did not log the message this filter targets, got %q", got)
	}

	var filtered syncBuffer
	routeStdlibLog(&filtered)
	provokeUnsolicited(t)
	if got := filtered.String(); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
}

// provokeUnsolicited makes one request against a server that writes junk on the
// connection after the response, and returns once the transport has logged
// something or given up waiting.
func provokeUnsolicited(t *testing.T) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	idle := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
			return
		}
		io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		<-idle
		io.WriteString(conn, "\x00\x01not a response at all")
		// Held open: closing here would race the peek and be read as a plain
		// idle close instead.
		time.Sleep(time.Second)
	}()

	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close() // the connection goes back to the pool, idle
	close(idle)

	// The read loop notices out of band, so there is nothing to wait on.
	time.Sleep(500 * time.Millisecond)
	<-done
}
