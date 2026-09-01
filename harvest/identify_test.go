package harvest

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/miku/metha/oai"
)

// TestEncodingSuspect is the table behind the most expensive line in a sweep.
// The identity-encoding workaround costs a second request, and a second request
// at a host that never answered the first one costs the dial timeout again -
// measured at 20.0s per unreachable endpoint against a 10s dial timeout, which
// was 51 of one 200-endpoint sweep's 68 worker-minutes.
func TestEncodingSuspect(t *testing.T) {
	// A dial timeout as the net package builds one: the timeout is a property
	// of the wrapped error, so a stand-in that merely says "i/o timeout" in its
	// text would pin a belief about the string rather than about the error.
	dial := &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nothing went wrong", nil, false},
		// Nothing arrived, so there is no encoding to blame.
		{"dial timeout", dial, false},
		{"dial timeout as the client wraps it",
			&url.Error{Op: "Get", URL: "http://a.test/oai", Err: dial}, false},
		{"name does not resolve", &net.DNSError{Err: "no such host", IsNotFound: true}, false},
		// A server was there and dropped us; that is what the workaround is for.
		{"connection reset",
			&net.OpError{Op: "read", Err: errors.New("connection reset by peer")}, true},
		// The server answered; it just answered with a status.
		{"http error", oai.HTTPError{StatusCode: 503}, false},
		{"http error wrapped", fmt.Errorf("identify: %w", oai.HTTPError{StatusCode: 403}), false},
		// Ours, and no request of any shape will fix it.
		{"cancelled", context.Canceled, false},
		{"deadline", fmt.Errorf("request: %w", context.DeadlineExceeded), false},
		// A response we could not read, which is what the workaround is for.
		{"unreadable document", oai.ErrParseFailed, true},
		{"truncated body", io.ErrUnexpectedEOF, true},
		{"broken gzip", errors.New("failed to decompress gzip data: unexpected EOF"), true},
		// Read fine, and there was too much of it. Asking again without
		// compression sends the same bytes uncompressed and is refused in the
		// same place, so the second request buys nothing.
		{"response too large", oai.ErrResponseTooLarge, false},
		{"response too large, wrapped",
			fmt.Errorf("identify: %w", oai.ErrResponseTooLarge), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := encodingSuspect(test.err); got != test.want {
				t.Errorf("encodingSuspect(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}

// countingDoer answers with whatever the test scripted, and remembers what it
// was asked.
type countingDoer struct {
	reply    func(n int, req *http.Request) (string, error)
	requests []*http.Request
}

func (d *countingDoer) Do(req *http.Request) (*http.Response, error) {
	d.requests = append(d.requests, req)
	body, err := d.reply(len(d.requests), req)
	if err != nil {
		return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: err}
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// identifyXML is a response that satisfies Identify.
func identifyXML(t *testing.T) string {
	t.Helper()
	b, err := xml.Marshal(oai.Response{Identify: oai.Identify{
		RepositoryName:    "test",
		ProtocolVersion:   "2.0",
		EarliestDatestamp: "2026-08-30",
		Granularity:       "YYYY-MM-DD",
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestIdentifyDoesNotRetryAnUnreachableHost: one request, not two. The second
// one used to be spent asking a host that had not answered its SYN to please
// answer without gzip.
func TestIdentifyDoesNotRetryAnUnreachableHost(t *testing.T) {
	d := &countingDoer{reply: func(int, *http.Request) (string, error) {
		return "", &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	}}
	_, err := NewHarvestWithClient(context.Background(), "http://gone.invalid/oai", &oai.Client{Doer: d})
	if err == nil {
		t.Fatal("an unreachable host produced no error")
	}
	if len(d.requests) != 1 {
		t.Errorf("identify made %d requests at an unreachable host, want 1", len(d.requests))
	}
}

// TestIdentifyRetriesWithIdentityEncoding: the workaround still fires where it
// was meant to - a response that arrived and could not be read - and the header
// it sets stays on the config, which is where the sweep reads the quirk from.
func TestIdentifyRetriesWithIdentityEncoding(t *testing.T) {
	d := &countingDoer{reply: func(n int, _ *http.Request) (string, error) {
		if n == 1 {
			return "<html>this is not an OAI response at all", nil
		}
		return identifyXML(t), nil
	}}
	h, err := NewHarvestWithClient(context.Background(), "http://quirky.test/oai", &oai.Client{Doer: d})
	if err != nil {
		t.Fatalf("the workaround did not recover the harvest: %v", err)
	}
	if len(d.requests) != 2 {
		t.Fatalf("identify made %d requests, want 2", len(d.requests))
	}
	if got := d.requests[1].Header.Get("Accept-Encoding"); got != "identity" {
		t.Errorf("the second request asked for %q, want identity", got)
	}
	if got := h.Config.ExtraHeaders.Get("Accept-Encoding"); got != "identity" {
		t.Errorf("the header did not stay on the config: %q", got)
	}
}
