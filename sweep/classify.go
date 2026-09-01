package sweep

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"os"
	"syscall"

	"github.com/miku/metha/harvest"
	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
)

// Classify says what one attempt on one endpoint meant. It is the endpoint-level
// counterpart of harvest's per-request classify, and like that one it is a pure
// function over an outcome with no configuration to consult.
//
// It reads harvest's real errors rather than a taxonomy invented beside them.
// That works because harvest is careful about what it returns: its fatal path
// hands back resp.Error directly, an oai.OAIError carrying the protocol code,
// and transport failures come out wrapped with %w through "failed to make
// request after retries", so errors.As reaches both.
//
// gained is how many records the cache gained; deadline reports whether the
// per-endpoint deadline fired, which the runner knows and the error does not -
// a context.DeadlineExceeded looks the same whether it came from our budget or
// from the client's own timeout.
//
// The second return says whether the attempt is worth recording at all. False
// means the endpoint was not really tried: another process held its shard, or
// the sweep itself was stopped. Writing a failure into the profile for either
// would leave a permanent mark for something that never went wrong - the same
// reasoning store/harvest uses when it declines to record an aborted window.
func Classify(err error, gained int, deadline bool) (Class, bool) {
	// A shard already being harvested is a skip, not an outcome. It happens
	// whenever someone runs metha sync by hand during a sweep, and it should
	// cost exactly nothing.
	if errors.Is(err, store.ErrLocked) {
		return "", false
	}
	if err == nil {
		if gained > 0 {
			return ClassOK, true
		}
		return ClassEmpty, true
	}
	// Nothing left to fetch. The endpoint is fully harvested and healthy; it is
	// ok rather than empty because we never asked it a question it answered
	// with nothing.
	if errors.Is(err, harvest.ErrAlreadySynced) {
		return ClassOK, true
	}
	// Our own deadline is an outcome: a slow endpoint is a fact about the
	// endpoint worth writing down.
	if deadline {
		return ClassTimeout, true
	}
	// Cancellation is never inferred from the error, because it cannot be.
	// http.Client.Timeout reports "context deadline exceeded (Client.Timeout
	// exceeded while awaiting headers)", and errors.Is finds
	// context.DeadlineExceeded in it - so an ordinary request timeout against a
	// host that blackholes packets is, to errors.Is, identical to this sweep
	// being stopped.
	//
	// Reading it as cancellation threw away the outcome for exactly the
	// endpoints the roster is for. Measured over 300 real endpoints, 64 of 184
	// were discarded this way, nearly all of them unreachable hosts: they would
	// have been re-attempted at full cost every sweep, for ever, with nothing
	// anywhere recording that they had ever been tried. Whether the sweep was
	// stopped is something only the runner knows, and it now says so rather
	// than leaving it to be guessed; a plain Canceled stays here as a
	// defensive case for callers outside the runner.
	if errors.Is(err, context.Canceled) {
		return "", false
	}

	// Order matters from here down, because the categories overlap as types. A
	// *net.DNSError is a net.Error, and so is the *net.OpError wrapping
	// ECONNREFUSED, so the permanent transport failures have to be recognised
	// before the transient ones swallow them.
	var dns *net.DNSError
	if errors.As(err, &dns) && dns.IsNotFound {
		return ClassGone, true
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return ClassGone, true
	}
	var httpErr oai.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case 401, 403:
			return ClassRefused, true
		case 404, 410:
			return ClassGone, true
		case 408, 429, 500, 502, 503, 504:
			return ClassTransient, true
		}
		// Some other status is still an answer from something that is there.
		// Treating it as transient rather than as a death is deliberate: the
		// backoff walks it out either way, and a live endpoint behind a 418 is
		// cheaper to keep than to lose.
		return ClassTransient, true
	}
	// A URL that answered, but not as an endpoint. harvest catches this before
	// a shard is ever opened, which is why it is by far the commonest protocol
	// case at corpus scale: a third of contrib/sites.tsv has no "oai" anywhere
	// in it, and a good many of those are home pages.
	if errors.Is(err, harvest.ErrNotAnEndpoint) {
		return ClassProtocol, true
	}
	var oaiErr oai.OAIError
	if errors.As(err, &oaiErr) {
		switch oaiErr.Code {
		case "badArgument", "badVerb", "cannotDisseminateFormat":
			// The URL is not an endpoint as asked. Often a quirk rather than a
			// death - an endpoint that cannot take a from and until, or that
			// chokes on the format parameter - which is what the eventual probe
			// is for.
			return ClassProtocol, true
		}
		return ClassTransient, true
	}
	// Garbage where a response was expected. oai reports it as a syntax error
	// when the document does not even claim to be XML, and as ErrParseFailed
	// when every encoding declaration it tried failed to parse.
	var syntax *xml.SyntaxError
	if errors.As(err, &syntax) || errors.Is(err, oai.ErrParseFailed) {
		return ClassProtocol, true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return ClassTransient, true
	}
	// A request that ran out of time. Transient rather than gone, because a
	// timeout cannot tell a host that is never coming back from one having a
	// bad afternoon; the backoff walks it out either way, and the seven-day cap
	// is the safe side to be wrong on.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return ClassTransient, true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return ClassTransient, true
	}
	// The default is the class that comes back soonest, on purpose. An error
	// this function does not recognise is a gap in this function, and the cost
	// of guessing wrong should fall on our request budget rather than on an
	// endpoint that gets buried for a month.
	return ClassTransient, true
}
