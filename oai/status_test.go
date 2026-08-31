package oai

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// countingBody answers a request and remembers whether the caller closed it.
type countingBody struct {
	io.Reader
	closed int
}

func (b *countingBody) Close() error {
	b.closed++
	return nil
}

// statusDoer answers with one status and one body.
type statusDoer struct {
	status int
	body   *countingBody
}

func (d *statusDoer) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: d.status, Body: d.body, Header: make(http.Header)}, nil
}

// TestErrorStatusClosesTheBody: an error status is still a response, and its
// body still holds the connection it was read on.
//
// The close was deferred after the status check rather than before it, so every
// 4xx and 5xx leaked one - and a harvest meets those in runs, since 500, 502,
// 503 and 504 are the statuses retryable asks to repeat. Over a sweep of a
// quarter of a million endpoints that is the file descriptor limit.
func TestErrorStatusClosesTheBody(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusServiceUnavailable} {
		body := &countingBody{Reader: strings.NewReader("<html>down for maintenance</html>")}
		c := &Client{Doer: &statusDoer{status: status, body: body}}
		_, err := c.Do(&Request{BaseURL: "http://example.com/oai", Verb: "ListRecords", MetadataPrefix: "oai_dc"})
		var httpErr HTTPError
		if !errors.As(err, &httpErr) {
			t.Fatalf("status %d: got %v, want an HTTPError", status, err)
		}
		if httpErr.StatusCode != status {
			t.Errorf("HTTPError.StatusCode = %d, want %d", httpErr.StatusCode, status)
		}
		if body.closed != 1 {
			t.Errorf("status %d: body closed %d time(s), want 1", status, body.closed)
		}
		// And the message says what happened rather than trailing a "<nil>":
		// nothing failed at the transport level, so there is no cause to print.
		if strings.Contains(httpErr.Error(), "<nil>") {
			t.Errorf("HTTPError.Error() = %q, want no nil cause", httpErr.Error())
		}
	}
}
