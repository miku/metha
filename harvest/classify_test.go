package harvest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/miku/metha/oai"
)

// TestRetryable is the table the retry policy used to be spread over a hundred
// lines to avoid having. Two things it pins that the old shouldRetry got wrong:
//
//   - a transient failure is retryable whether or not -ignore-http-errors was
//     given. The old form asked the flag first and answered no, so an operator
//     who wanted retries had to ask for something else to get them;
//   - a transport failure is recognised by its type, not by its text. The old
//     form matched strings.Contains(err.Error(), "timeout"), which missed every
//     phrasing it was not written for and matched anything that happened to
//     mention the word.
func TestRetryable(t *testing.T) {
	// The shapes a transport failure actually arrives in, rather than errors
	// whose text resembles one.
	refused := &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
	noHost := &net.DNSError{Err: "no such host", Name: "nope.invalid", IsNotFound: true}
	timedOut := &url.Error{Op: "Get", URL: "http://example.com", Err: &net.DNSError{IsTimeout: true}}

	tests := []struct {
		name string
		err  error
		resp *oai.Response
		want bool
	}{
		{"a plain response", nil, &oai.Response{}, false},
		{"HTTP 408 request timeout", oai.HTTPError{StatusCode: 408}, nil, true},
		{"HTTP 429 too many requests", oai.HTTPError{StatusCode: 429}, nil, true},
		{"HTTP 500 internal server error", oai.HTTPError{StatusCode: 500}, nil, true},
		{"HTTP 502 bad gateway", oai.HTTPError{StatusCode: 502}, nil, true},
		{"HTTP 503 service unavailable", oai.HTTPError{StatusCode: 503}, nil, true},
		{"HTTP 504 gateway timeout", oai.HTTPError{StatusCode: 504}, nil, true},
		// An answer, and it will be the same next time.
		{"HTTP 404 not found", oai.HTTPError{StatusCode: 404}, nil, false},
		{"HTTP 401 unauthorized", oai.HTTPError{StatusCode: 401}, nil, false},
		{"unexpected EOF", io.ErrUnexpectedEOF, nil, true},
		{"wrapped unexpected EOF", fmt.Errorf("reading body: %w", io.ErrUnexpectedEOF), nil, true},
		{"connection refused", refused, nil, true},
		{"name does not resolve", noHost, nil, true},
		{"timed out, wrapped by url.Error", timedOut, nil, true},
		// #9717: the endpoint stumbling rather than answering.
		{"InternalException", nil, &oai.Response{Error: oai.OAIError{Code: "InternalException"}}, true},
		// Well-formed OAI errors are answers.
		{"noRecordsMatch", nil, &oai.Response{Error: oai.OAIError{Code: "noRecordsMatch"}}, false},
		{"badArgument", nil, &oai.Response{Error: oai.OAIError{Code: "badArgument"}}, false},
		// Repeating a cancelled request cannot un-cancel it.
		{"cancelled", context.Canceled, nil, false},
		{"deadline exceeded", context.DeadlineExceeded, nil, false},
		// Text that merely resembles a network failure is not one.
		{"an error that says timeout", errors.New("timeout"), nil, false},
		{"some other error", errors.New("some other error"), nil, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := retryable(test.err, test.resp); got != test.want {
				t.Errorf("retryable(%v, %v) = %v, want %v", test.err, test.resp, got, test.want)
			}
		})
	}
}

// TestClassify pins the other half of the split: what a failure that outlived
// its retries, or a response that carries an OAI error, means for the harvest.
// This is where -ignore-http-errors belongs and the only place it is consulted.
func TestClassify(t *testing.T) {
	var (
		strict  = &Config{}
		lenient = &Config{IgnoreHTTPErrors: true}
		eofOK   = &Config{IgnoreUnexpectedEOF: true}
	)
	tests := []struct {
		name string
		cfg  *Config
		err  error
		resp *oai.Response
		want action
	}{
		{"a response with records", strict, nil, &oai.Response{}, keep},
		// The two policies, told apart: the same failure ends the harvest or
		// costs one window, and only the flag decides which.
		{"a dead endpoint, strictly", strict, oai.HTTPError{StatusCode: 500}, nil, fatal},
		{"a dead endpoint, ignoring HTTP errors", lenient, oai.HTTPError{StatusCode: 500}, nil, skipWindow},
		{"a truncated response, strictly", strict, io.ErrUnexpectedEOF, nil, fatal},
		{"a truncated response, ignoring EOF", eofOK, io.ErrUnexpectedEOF, nil, skipWindow},
		// A cancelled harvest stops, and no flag talks it out of that: skipping
		// to the next window would be a way of ignoring the operator.
		{"cancelled, strictly", strict, context.Canceled, nil, fatal},
		{"cancelled, ignoring HTTP errors", lenient, context.Canceled, nil, fatal},
		{"cancelled, ignoring EOF", eofOK, context.Canceled, nil, fatal},
		// An empty range is an answer: the window commits with what it has,
		// which for a window that found nothing at all is a row and no bytes.
		{"noRecordsMatch", strict, nil, &oai.Response{Error: oai.OAIError{Code: "noRecordsMatch"}}, done},
		{"noRecordsMatch with a token", strict, nil, &oai.Response{
			Error:       oai.OAIError{Code: "noRecordsMatch"},
			ListRecords: oai.ListRecords{ResumptionToken: oai.ResumptionToken{Text: "more"}},
		}, keep},
		{"badResumptionToken", strict, nil, &oai.Response{Error: oai.OAIError{Code: "badResumptionToken"}}, done},
		// Anything else is the endpoint refusing the question as asked, and
		// asking it again for the next window would refuse just the same.
		{"badArgument", strict, nil, &oai.Response{Error: oai.OAIError{Code: "badArgument"}}, fatal},
		{"cannotDisseminateFormat", lenient, nil, &oai.Response{Error: oai.OAIError{Code: "cannotDisseminateFormat"}}, fatal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := classify(test.cfg, test.err, test.resp)
			if got != test.want {
				t.Errorf("classify(%v, %v) = %v, want %v", test.err, test.resp, got, test.want)
			}
		})
	}
}

// TestIdentifyRejectsNonEndpoints: a URL that is not an OAI-PMH endpoint still
// answers - a home page, a login form, a 200 carrying an error page - and the
// decoder is lenient by design, so all of them come back as an Identify with
// nothing in it rather than as a failure. Catching that is what keeps a mistyped
// URL from reaching the disk: NewHarvest runs before a writer is opened.
//
// The test is a table over what endpoints actually send, because the check has
// to be permissive in one direction only. An endpoint that answers with a
// repository name and nothing else is broken but real, and -no-intervals can
// still harvest it whole; refusing that would take working endpoints away.
func TestIdentifyRejectsNonEndpoints(t *testing.T) {
	for _, tt := range []struct {
		name   string
		id     oai.Identify
		reject bool
	}{
		{"a home page, decoded into nothing", oai.Identify{}, true},
		{"a full identify", oai.Identify{
			RepositoryName:    "Test",
			ProtocolVersion:   "2.0",
			Granularity:       "YYYY-MM-DD",
			EarliestDatestamp: "2020-01-01",
		}, false},
		// Each of these is broken in some way, and each is still an endpoint
		// that said something. -no-intervals plans one boundless window without
		// asking about dates at all, so none of them may be refused here.
		{"a name and nothing else", oai.Identify{RepositoryName: "Test"}, false},
		{"no granularity", oai.Identify{RepositoryName: "Test", ProtocolVersion: "2.0"}, false},
		{"granularity only", oai.Identify{Granularity: "YYYY-MM-DD"}, false},
		{"an admin address only", oai.Identify{AdminEmail: []string{"a@example.com"}}, false},
		{"a base URL only", oai.Identify{BaseURL: "http://example.com/oai"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.id.IsEmpty(); got != tt.reject {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.reject)
			}
		})
	}
}
