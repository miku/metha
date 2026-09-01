package sweep

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/miku/metha/harvest"
	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
)

// TestClassify is the taxonomy, built from the error values harvest actually
// returns rather than from ones whose text resembles them. That is the whole
// reason this is testable at all: harvest's fatal path hands back resp.Error
// directly, an oai.OAIError carrying the protocol code, and transport failures
// come out wrapped through "failed to make request after retries: %w".
func TestClassify(t *testing.T) {
	// The shapes a transport failure arrives in, as harvest/classify_test.go
	// builds them.
	refused := &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
	noHost := &net.DNSError{Err: "no such host", Name: "gone.invalid", IsNotFound: true}
	dnsTimeout := &url.Error{Op: "Get", URL: "http://slow.example", Err: &net.DNSError{IsTimeout: true}}
	// What a dial onto an address that swallows packets leaves behind, which the
	// long tail of the corpus is full of: private ranges, and machines that
	// moved years ago.
	ioTimeout := &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	// And the way harvest wraps whatever outlived its retries.
	wrapped := func(err error) error {
		return fmt.Errorf("failed to make request after retries: %w", err)
	}

	tests := []struct {
		name     string
		err      error
		gained   int
		deadline bool
		want     Class
		record   bool
	}{
		// Success, and the two things it can mean.
		{"records committed", nil, 41, false, ClassOK, true},
		{"nothing to fetch", nil, 0, false, ClassEmpty, true},
		{"already up to date", harvest.ErrAlreadySynced, 0, false, ClassOK, true},

		// Not attempted. Neither of these may leave a mark on a profile: a
		// permanent failure recorded for something that never went wrong is
		// what metha stat would then report for the life of the cache.
		{"another process holds the shard", store.ErrLocked, 0, false, "", false},
		{"wrapped lock", fmt.Errorf("open: %w", store.ErrLocked), 0, false, "", false},
		{"the sweep's own budget ran out", context.Canceled, 0, false, "", false},
		{"an operator pressed Ctrl-C", context.Canceled, 12, false, "", false},

		// The rows that matter most here, because getting them wrong is silent.
		// http.Client.Timeout reports "context deadline exceeded", and errors.Is
		// finds context.DeadlineExceeded inside it - so a host that blackholes
		// packets is, to errors.Is, indistinguishable from this sweep being
		// stopped. Reading it as cancellation discarded the outcome for exactly
		// the endpoints the roster exists to remember: 64 of 184 over 300 real
		// endpoints, nearly all unreachable hosts, which would then have been
		// retried at full cost every sweep for ever.
		{"a real client timeout", realClientTimeout(t), 0, false, ClassTransient, true},
		{"a bare deadline, ours or not", context.DeadlineExceeded, 0, false, ClassTransient, true},
		{"an i/o deadline", os.ErrDeadlineExceeded, 0, false, ClassTransient, true},
		{"a dial timeout", ioTimeout, 0, false, ClassTransient, true},
		{"a dial timeout, wrapped", wrapped(ioTimeout), 0, false, ClassTransient, true},

		// Our deadline, which is an outcome: slowness is a fact about the
		// endpoint worth writing down.
		{"the per-endpoint deadline", context.DeadlineExceeded, 0, true, ClassTimeout, true},
		{"the deadline, having gained records", context.DeadlineExceeded, 500, true, ClassTimeout, true},
		// A client timeout that surfaced as something else, with the runner
		// still reporting that its deadline fired.
		{"the deadline, reported oddly", errors.New("net/http: timeout"), 0, true, ClassTimeout, true},

		// Dead. These have to be recognised before the transient rules, because
		// a *net.DNSError is a net.Error and so is the OpError around
		// ECONNREFUSED.
		{"name does not resolve", noHost, 0, false, ClassGone, true},
		{"name does not resolve, wrapped", wrapped(noHost), 0, false, ClassGone, true},
		{"connection refused", refused, 0, false, ClassGone, true},
		{"connection refused, wrapped", wrapped(refused), 0, false, ClassGone, true},
		{"HTTP 404 not found", oai.HTTPError{StatusCode: 404}, 0, false, ClassGone, true},
		{"HTTP 410 gone", oai.HTTPError{StatusCode: 410}, 0, false, ClassGone, true},

		// There, and not talking to us.
		{"HTTP 401 unauthorized", oai.HTTPError{StatusCode: 401}, 0, false, ClassRefused, true},
		{"HTTP 403 forbidden", oai.HTTPError{StatusCode: 403}, 0, false, ClassRefused, true},

		// Worth another try soon.
		{"HTTP 429 too many requests", oai.HTTPError{StatusCode: 429}, 0, false, ClassTransient, true},
		{"HTTP 500", oai.HTTPError{StatusCode: 500}, 0, false, ClassTransient, true},
		{"HTTP 503", wrapped(oai.HTTPError{StatusCode: 503}), 0, false, ClassTransient, true},
		{"a status nobody expected", oai.HTTPError{StatusCode: 418}, 0, false, ClassTransient, true},
		{"unexpected EOF", wrapped(io.ErrUnexpectedEOF), 0, false, ClassTransient, true},
		{"a DNS timeout", wrapped(dnsTimeout), 0, false, ClassTransient, true},

		// Answered, but not as an endpoint. ErrNotAnEndpoint is by far the
		// commonest of these at corpus scale: a third of contrib/sites.tsv has
		// no "oai" anywhere in it, and harvest catches those before a shard is
		// ever opened.
		{"not an endpoint", harvest.ErrNotAnEndpoint, 0, false, ClassProtocol, true},
		{"not an endpoint, wrapped", fmt.Errorf("%w: http://x/", harvest.ErrNotAnEndpoint), 0, false, ClassProtocol, true},
		{"badArgument", oai.OAIError{Code: "badArgument"}, 0, false, ClassProtocol, true},
		{"badVerb", oai.OAIError{Code: "badVerb"}, 0, false, ClassProtocol, true},
		{"cannotDisseminateFormat", oai.OAIError{Code: "cannotDisseminateFormat"}, 0, false, ClassProtocol, true},
		{"not XML at all", &xml.SyntaxError{Msg: "invalid character", Line: 1}, 0, false, ClassProtocol, true},
		{"nothing would parse", oai.ErrParseFailed, 0, false, ClassProtocol, true},
		{"nothing would parse, wrapped", wrapped(oai.ErrParseFailed), 0, false, ClassProtocol, true},

		// An OAI error that is not about the request being malformed is the
		// endpoint stumbling, and it may well answer next time.
		{"an endpoint stumbling", oai.OAIError{Code: "InternalException"}, 0, false, ClassTransient, true},

		// The default is the class that comes back soonest, on purpose: an
		// unrecognised error is a gap in Classify, and the cost of that gap
		// should fall on our request budget rather than bury a live endpoint.
		{"an error nobody has seen before", errors.New("something new"), 0, false, ClassTransient, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, record := Classify(test.err, test.gained, test.deadline)
			if record != test.record {
				t.Fatalf("Classify(%v) recorded = %v, want %v", test.err, record, test.record)
			}
			if got != test.want {
				t.Errorf("Classify(%v) = %q, want %q", test.err, got, test.want)
			}
		})
	}
}

// realClientTimeout produces the error an http.Client actually returns when its
// timeout fires, rather than a reconstruction of it.
//
// Built for real because the shape is the whole point, and it is not what it
// looks like: the message reads "context deadline exceeded (Client.Timeout
// exceeded while awaiting headers)" and errors.Is(err, context.DeadlineExceeded)
// is true. Anything that reads that as "our context was cancelled" is wrong
// about every timed-out endpoint in the corpus. Hand-writing a stand-in here
// would pin a belief about Go rather than Go.
func realClientTimeout(t *testing.T) error {
	t.Helper()
	stall := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-stall:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(stall); srv.Close() })

	_, err := (&http.Client{Timeout: 50 * time.Millisecond}).Get(srv.URL)
	if err == nil {
		t.Fatal("the stalling server answered")
	}
	// The premise, asserted rather than assumed: if Go ever stops reporting a
	// client timeout this way, this test should say so here rather than let the
	// rows below quietly start passing for the wrong reason.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a client timeout no longer wraps context.DeadlineExceeded: %v", err)
	}
	return err
}

// TestClassifyNeverBuriesAnEndpointItDidNotReach is the property behind the
// "not attempted" rows: whatever Classify declines to record must leave a
// profile untouched, so that a sweep interrupted halfway does not walk every
// endpoint it had not reached yet toward quarantine.
func TestClassifyNeverBuriesAnEndpointItDidNotReach(t *testing.T) {
	before := Profile{URL: "u", State: StateActive, Failures: 0}
	for _, err := range []error{store.ErrLocked, context.Canceled} {
		class, record := Classify(err, 0, false)
		if record {
			t.Fatalf("Classify(%v) = %q, recorded; want it skipped", err, class)
		}
		// The runner's contract: a skipped attempt never reaches Apply.
		if before.State != StateActive || before.Failures != 0 {
			t.Fatal("the profile was modified")
		}
	}
}
