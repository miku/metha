package harvest

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/miku/metha/oai"
)

// A harvest fails in a small number of ways and there are only four things it
// can do about any of them. Deciding which was spread over a hundred lines of
// the request loop, mixed in with the loop's own bookkeeping; this file is that
// decision on its own, as two pure functions over a request's outcome.
//
// The split matters more than the tidying. "Repeat this request, it might work"
// and "this failure is not worth ending the harvest over" are independent
// policies, and the old shouldRetry conflated them: it would not retry anything
// at all unless -ignore-http-errors was given, so an operator who wanted a
// harvest to survive a dead window had to ask for it by asking for something
// else. Retrying is now what retryable says, always; -ignore-http-errors decides
// only what happens to a failure that outlived the retries.

// action is what the driver does with one request's outcome.
type action int

const (
	// keep stores the response and follows its resumption token: the ordinary
	// outcome, and the only one that adds bytes to the cache.
	keep action = iota
	// done ends the window here, keeping what it already holds. The response
	// itself is not worth storing - it is the endpoint saying there is nothing
	// more - but the range was asked and answered, so it commits.
	done
	// skipWindow gives up on this window and moves to the next. The range stays
	// uncovered, so the next run plans it again.
	skipWindow
	// fatal ends the harvest.
	fatal
)

// errSkipWindow marks the error a skipped window returns, so that run can tell
// it apart from a failure without inspecting what caused it.
var errSkipWindow = errors.New("skipping window")

// retryable reports whether repeating a request verbatim could produce a
// different outcome. This is the whole of the retry policy, and it deliberately
// asks nothing about configuration: a timeout is a timeout whether or not the
// operator is willing to lose the window it belongs to.
func retryable(err error, resp *oai.Response) bool {
	if err == nil {
		// #9717, "InternalException Could not send Message": the endpoint
		// stumbling over itself rather than answering the question. Everything
		// else that arrives as a well-formed OAI error is an answer, and asking
		// again gets the same one.
		return resp != nil && resp.Error.Code == "InternalException"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// A cancelled harvest is not a failed request; nothing about it changes by
	// being asked again.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var httpErr oai.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case 408, // Request Timeout
			429, // Too Many Requests
			500, // Internal Server Error
			502, // Bad Gateway
			503, // Service Unavailable
			504: // Gateway Timeout
			return true
		}
		// Any other status is an answer, and it will be the same next time.
		return false
	}
	// Anything the net package reports is a transport failure: a refused
	// connection, a name that did not resolve, a timeout. This replaces three
	// strings.Contains checks against error text, which missed every phrasing
	// they were not written for and matched any error that happened to contain
	// the word.
	var netErr net.Error
	return errors.As(err, &netErr)
}

// classify says what one request's outcome means for the harvest, once the
// retries it was worth have been spent. The string it returns alongside is what
// to log about the decision, empty when there is nothing worth saying.
func classify(cfg *Config, err error, resp *oai.Response) (action, string) {
	if err != nil {
		// A cancelled harvest stops now, and stops everywhere: the operator is
		// waiting, and skipping to the next window would be a way of ignoring
		// them.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fatal, ""
		}
		switch {
		case cfg.IgnoreUnexpectedEOF && errors.Is(err, io.ErrUnexpectedEOF):
			return skipWindow, ""
		case cfg.IgnoreHTTPErrors:
			return skipWindow, ""
		}
		return fatal, ""
	}
	switch resp.Error.Code {
	case "":
		return keep, ""
	case "noRecordsMatch":
		// With a resumption token this is the odd case: the endpoint handed out
		// a token and then said the range is empty. The token is worth
		// following, since what it points at may not be.
		if resp.HasResumptionToken() {
			return keep, "resumptionToken set and noRecordsMatch, continuing"
		}
		return done, ""
	case "badResumptionToken":
		// Usually the endpoint's way of saying the harvest ran off the end of
		// what it was willing to page through. What has been fetched so far
		// stands.
		return done, "badResumptionToken, might signal end-of-harvest"
	default:
		return fatal, ""
	}
}
