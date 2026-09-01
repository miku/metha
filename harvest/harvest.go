package harvest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
)

// Day has 24 hours.
const Day = 24 * time.Hour

// SettleLag is how far back from the clock a harvest stops trusting an endpoint
// that stamps records to the second. Endpoints index a record a moment after
// they stamp it and their clocks do not agree with ours, so the last stretch
// before now is refetched on the next run rather than assumed complete.
const SettleLag = 5 * time.Minute

var (
	// ErrAlreadySynced signals completion.
	ErrAlreadySynced = errors.New("already synced")
	// ErrNoWriter is returned by Run when there is nowhere to write.
	ErrNoWriter = errors.New("harvest needs a writer")
	// errNoRawResponse guards the one thing a harvest must never do quietly:
	// store a response it does not have the bytes of. oai.Client fills Raw on
	// every successful decode, so this can only be a Response built by hand -
	// and writing a re-marshalled stand-in for it would put a document in the
	// cache that is not what any endpoint said.
	errNoRawResponse = errors.New("response carries no raw document")
	// ErrNotAnEndpoint marks a URL that answered, but not as an OAI-PMH
	// endpoint. It is a distinct error because it is almost always a typo in a
	// URL rather than anything wrong with the cache, and because it has to be
	// known before a shard is opened - see identify.
	ErrNotAnEndpoint = errors.New("not an OAI-PMH endpoint")
)

type Config struct {
	BaseURL                    string
	Format                     string
	Set                        string
	From                       string
	Until                      string
	MaxRequests                int
	DisableSelectiveHarvesting bool
	CleanBeforeDecode          bool
	IgnoreHTTPErrors           bool
	MaxEmptyResponses          int
	SuppressFormatParameter    bool
	HourlyInterval             bool
	DailyInterval              bool
	ExtraHeaders               http.Header
	IgnoreUnexpectedEOF        bool
	Delay                      time.Duration
	MaxRetries                 int           // Maximum number of retry attempts
	RetryDelay                 time.Duration // Delay between retries
	RetryBackoff               float64       // Multiplier for delay between retries (e.g., 2.0 for exponential backoff)
}

// Harvest contains parameters for mass-download. MaxRequests and
// CleanBeforeDecode are switches to handle broken token implementations and
// funny chars in responses. Some repos do not support selective harvesting
// (e.g. zvdd.org/oai2). Set "DisableSelectiveHarvesting" to try to grab
// metadata from these repositories. From and Until must always be given with
// 2006-01-02 layout. TODO(miku): make zero type work (lazily run identify).
type Harvest struct {
	Config *Config
	Client *oai.Client

	// Writer is the shard the harvested responses go into. The caller owns it,
	// and closes it.
	Writer *store.Writer

	// XXX: Lazy via sync.Once?
	Identify *oai.Identify
	Started  time.Time
}

// NewHarvest creates a new harvest. A network connection will be used for an
// initial Identify request, made with oai.DefaultClient.
//
// Deprecated for anything that has a client of its own: use
// NewHarvestWithClient. Setting Client on the result is too late, because the
// Identify has already been made by then - see there for what that cost.
func NewHarvest(ctx context.Context, baseURL string) (*Harvest, error) {
	return NewHarvestWithClient(ctx, baseURL, nil)
}

// NewHarvestWithClient creates a harvest that identifies with the given client.
// A nil client means oai.DefaultClient.
//
// The distinction matters more than it looks. Identify is the first request a
// harvest makes and by far the likeliest to fail - a dead host fails here and
// nowhere else - so it is the one request whose timeout and retry count decide
// what a bad URL costs. Assigning Client after NewHarvest, which is what every
// caller did, left that request on the default client's eight retries with
// exponential backoff and its ten-minute timeout, whatever the caller had
// asked for.
//
// The cost was measured, before it was understood: "metha sync http:// --retries
// 2 --timeout 3s" was still retrying after 249 seconds. Eight doubling waits
// from a second is 255, which is where the number came from. Neither flag was
// reaching the request that was spending the time.
func NewHarvestWithClient(ctx context.Context, baseURL string, client *oai.Client) (*Harvest, error) {
	h := Harvest{
		Client: client,
		Config: &Config{
			BaseURL:      baseURL,
			MaxRetries:   3,
			RetryDelay:   10 * time.Second,
			RetryBackoff: 2.0,
		},
	}
	if err := h.identify(ctx); err != nil {
		return nil, err
	}
	return &h, nil
}

// utcSecond is the second granularity form of an OAI datestamp. The trailing Z
// is a literal here, not a zone, which is why formatBound moves the instant to
// UTC itself: the protocol defines this form as UTC, so anything else would
// label a local wall clock as though it were.
const utcSecond = "2006-01-02T15:04:05Z"

// formatBound renders a window boundary as the endpoint's advertised
// granularity spells it, or the empty string when the endpoint said nothing
// intelligible - which is what leaves a request without bounds, as it always
// has been.
//
// Second granularity is UTC by definition, so the instant moves there first. A
// date is not an instant and must not: the boundary was computed in the local
// zone, and shifting it would name the day before or after the one meant.
func (h *Harvest) formatBound(t time.Time) string {
	switch {
	case h.Identify.SecondGranularity():
		return t.UTC().Format(utcSecond)
	case h.Identify.DayGranularity():
		return t.Format("2006-01-02")
	}
	return ""
}

// Run starts the harvest. The writer owns the shard directory and its lock, so
// there is nothing to prepare here beyond the clock.
//
// Cancelling ctx stops the harvest between two requests: the window in flight
// is aborted, the caller's deferred Close releases the shard lock, and the run
// returns ctx.Err(). There is no signal handler here any more and no mutex
// guarding the writer - a cancellable loop leaves no second goroutine to race a
// commit, so there is nothing left to exclude. That was move 5 of the
// simplification note, and the mutex it deleted was only ever compensating for
// the handler.
func (h *Harvest) Run(ctx context.Context) error {
	if h.Writer == nil {
		return ErrNoWriter
	}
	h.Started = time.Now()
	return h.run(ctx)
}

// planConfig is the part of this harvest's configuration that decides what to
// fetch, as opposed to how to ask for it or where to put it.
func (h *Harvest) planConfig() PlanConfig {
	cfg := PlanConfig{
		From:      h.Config.From,
		Until:     h.Config.Until,
		Unbounded: h.Config.DisableSelectiveHarvesting,
	}
	switch {
	case h.Config.HourlyInterval:
		cfg.Segmentation = Hourly
	case h.Config.DailyInterval:
		cfg.Segmentation = Daily
	}
	return cfg
}

// coverage asks the writer how far it got. It is the one question the plan puts to
// the disk, and the reason the plan itself needs none: a store that can tell a
// settled window from one holding only what existed at the moment of asking
// hands back the start of the latter, so its range is covered again rather than
// resumed past.
func (h *Harvest) coverage() Coverage {
	return Coverage{Resume: h.Writer.Resume()}
}

// retry runs one request, repeating it with exponential backoff for as long as
// the outcome is one that repeating could change. What "could change" means is
// retryable, and it is the whole of the retry policy; whether a failure that
// survives the retries ends the harvest is a different question, answered by
// classify.
//
// The waiting is cancellable, which is most of why a Ctrl-C is now prompt: the
// old form slept through its backoff and could only notice a signal after it.
func (h *Harvest) retry(ctx context.Context, operation func() (*oai.Response, error)) (*oai.Response, error) {
	var lastErr error
	delay := h.Config.RetryDelay
	for attempt := 0; attempt <= h.Config.MaxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("retry attempt %d/%d after %v", attempt, h.Config.MaxRetries, delay)
			if err := sleep(ctx, delay); err != nil {
				return nil, err
			}
			// Apply backoff for next attempt
			delay = time.Duration(float64(delay) * h.Config.RetryBackoff)
		}
		resp, err := operation()
		if !retryable(err, resp) {
			return resp, err
		}
		if err != nil {
			lastErr = err
			log.Printf("request failed (attempt %d/%d): %v", attempt+1, h.Config.MaxRetries, err)
		} else {
			lastErr = resp.Error
			log.Printf("endpoint returned %s (attempt %d/%d)", resp.Error.Code, attempt+1, h.Config.MaxRetries)
		}
	}
	return nil, fmt.Errorf("failed after %d retries: %w", h.Config.MaxRetries, lastErr)
}

// sleep waits, or gives up early when the harvest is cancelled. Every wait in a
// harvest goes through it, so there is no stretch of a run that a Ctrl-C has to
// sit out.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run plans the harvest and works through it, one window at a time.
func (h *Harvest) run(ctx context.Context) (err error) {
	cfg := h.planConfig()
	var cov Coverage
	if !cfg.Unbounded {
		// An unbounded harvest covers whatever the endpoint chooses to send and
		// resumes from nothing, so it never asks.
		cov = h.coverage()
	}
	windows, err := Plan(cov, h.Identify, h.Started, cfg)
	if err != nil {
		return fmt.Errorf("failed to plan harvest: %w", err)
	}
	if n := len(windows); n > 0 && !windows[0].Boundless() {
		log.Printf("plan: %d window(s), %v to %v", n, windows[0].Begin, windows[n-1].End)
	}

	for _, w := range windows {
		if err := ctx.Err(); err != nil {
			return err
		}
		// A range some settled window already covers is not fetched again. The
		// plan resumes from the earliest window that is not final, which is how a
		// failure in the middle of a range gets retried at all - but everything
		// after that failure has usually been fetched already, and without this
		// the whole tail is refetched on every run until the bad window succeeds.
		//
		// It is also what keeps the index a coverage map. A refetched window
		// replaces the row that matches it exactly; where the segmentation
		// changed between runs - -daily added to an endpoint that got slow -
		// nothing matches, and the new row is simply added beside the settled one
		// it overlaps, so the records under the overlap are indexed twice and
		// read twice.
		if !w.Boundless() && h.Writer.HasWindow(w.Begin, w.End) {
			log.Printf("already harvested, skipping: %v", w)
			continue
		}
		if err := h.runWindow(ctx, w); err != nil {
			// A window the operator said to skip leaves the range uncovered, so
			// the next run plans it again; the abort in runWindow already made
			// sure it left nothing half-written behind.
			if errors.Is(err, errSkipWindow) {
				log.Printf("skipping the rest of this window: %v", errors.Unwrap(err))
				continue
			}
			return err
		}
	}
	return nil
}

// runWindow fetches one window of the plan: one request plus every resumption
// token that follows from it.
func (h *Harvest) runWindow(ctx context.Context, w Window) (err error) {
	// A boundless window claims no range at all. Its bytes still accumulate, the
	// blob layer being append-only, but only the newest copy is indexed and so
	// only that one is ever read.
	if err := h.Writer.Begin(w.Begin, w.End, w.Settled); err != nil {
		return err
	}
	defer func() {
		// A window that did not reach Commit leaves nothing behind.
		if err == nil {
			return
		}
		// With one exception to what it records. An abort with a cause writes a
		// row saying the range was tried and failed, which is what a later run
		// needs to know about an endpoint that would not answer - but a
		// cancelled harvest is the operator stopping, not the endpoint failing.
		// Recording that would leave a permanent failure in the shard for
		// something that never went wrong, and metha stat would report it for
		// the life of the cache. Dropped without a row, the range is simply not
		// covered, which is what brings the next run back to it.
		cause := err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			cause = nil
		}
		err = errors.Join(err, h.Writer.Abort(cause))
	}()
	var token string
	var i, empty int
requests:
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Zero means no limit, and it has to be said: compared for equality, an
		// unset MaxRequests matched the i == 0 the loop starts at, so a harvest
		// configured in Go rather than through the flags broke before its first
		// request and committed the range as empty - the same shape of bug the
		// empty-response counter below had.
		if h.Config.MaxRequests > 0 && i >= h.Config.MaxRequests {
			log.Printf("max requests limit (%d) reached", h.Config.MaxRequests)
			break
		}
		req := oai.Request{
			BaseURL:                 h.Config.BaseURL,
			MetadataPrefix:          h.Config.Format,
			Verb:                    "ListRecords",
			Set:                     h.Config.Set,
			ResumptionToken:         token,
			CleanBeforeDecode:       h.Config.CleanBeforeDecode,
			SuppressFormatParameter: h.Config.SuppressFormatParameter,
			ExtraHeaders:            h.Config.ExtraHeaders,
		}
		// A boundless window asks for everything, which is what an endpoint
		// that cannot handle from and until is given.
		if !w.Boundless() {
			req.From = h.formatBound(w.Begin)
			req.Until = h.formatBound(w.End)
		}

		if err := sleep(ctx, h.Config.Delay); err != nil {
			return err
		}

		resp, err := h.retry(ctx, func() (*oai.Response, error) {
			return h.Client.DoContext(ctx, &req)
		})
		act, why := classify(h.Config, err, resp)
		if why != "" {
			log.Println(why)
		}
		switch act {
		case keep:
		case done:
			// This response says the window holds nothing more, so it is not
			// worth storing - but what the window already holds is. The range
			// was reached for and answered, which is what a commit records; a
			// window that answered with nothing at all commits as empty, which
			// costs a row and no bytes.
			break requests
		case skipWindow:
			return fmt.Errorf("%w: %w", errSkipWindow, err)
		case fatal:
			if err != nil {
				return fmt.Errorf("failed to make request after retries: %w", err)
			}
			return resp.Error
		}

		// What the endpoint sent, not what could be decoded from it. metha is a
		// cache of responses, and re-marshalling oai.Response wrote back only
		// the fields the decoder happened to have - every extension element,
		// every attribute nothing models, the response's own namespaces and
		// declaration, all dropped on the way in and unrecoverable afterwards.
		// It also wrote an empty skeleton for each of the five verbs a response
		// is not, so a one-record ListRecords reached the segment carrying a
		// phantom GetRecord.
		if len(resp.Raw) == 0 {
			return errNoRawResponse
		}
		if err := h.Writer.Append(resp.Raw); err != nil {
			return err
		}
		// Issue first observed at
		// https://gssrjournal.com/gssroai/?resumptionToken=33NjdYRs708&verb=ListRecords,
		// would spill the disk.
		prev := token
		if token = resp.GetResumptionToken(); token == "" {
			break
		}
		if prev == token {
			url, _ := req.URL()
			log.Printf("token %q did not change, assume server issue, moving to next window for: %s", token, url)
			break
		}
		i++
		if len(resp.ListRecords.Records) > 0 {
			empty = 0
		} else {
			empty++
			log.Printf("warning: successive empty response: %d/%d", empty, h.Config.MaxEmptyResponses)
		}
		// Only a run of empty responses ends a window, which is why the count
		// has to be non-zero before the limit is consulted: the old form
		// compared for equality, so an unset MaxEmptyResponses matched the
		// empty == 0 that every response carrying records leaves behind, and a
		// harvest configured in Go rather than through the flags stopped after
		// one request.
		if empty > 0 && empty >= h.Config.MaxEmptyResponses {
			log.Printf("max number of empty responses reached")
			break
		}
	}
	return h.Writer.Commit()
}

// encodingSuspect reports whether an error could plausibly be about how the
// endpoint encoded its answer, which is the only thing asking again with
// Accept-Encoding: identity can fix.
//
// The workaround used to fire on every error identify saw, and that is the most
// expensive line in a sweep. A host that does not answer its SYN costs the dial
// timeout to find out; retrying it costs the same again, for a header that
// cannot matter to a connection that was never made. Measured over 200 real
// endpoints from the embedded list: 149 came back unreachable, at 20.0s each -
// exactly twice the 10s dial timeout - and that was 51 of the sweep's 68
// worker-minutes. Half of it bought nothing.
//
// So the rule is what the workaround always meant: it applies to a response we
// could not read, not to a request that never arrived. Anything that reports
// itself as a network error - a dial or read timeout, a refused connection, a
// name that does not resolve, a reset - means no HTTP response came back, and
// an HTTPError means one did and said something else entirely. What is left is
// what the workaround was written for: a body that would not decompress, a
// truncated document, an unexpected EOF.
//
// A connection reset is the one exclusion worth arguing about, since a server
// that hates a header can drop the connection rather than answer. The evidence
// says it is not worth the second request: in the run above, every reset that
// was retried was reset again.
func encodingSuspect(err error) bool {
	if err == nil {
		return false
	}
	// Our own cancellation, which nothing about the endpoint can explain.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var nerr net.Error
	if errors.As(err, &nerr) {
		return false
	}
	var herr oai.HTTPError
	return !errors.As(err, &herr)
}

// identify runs an OAI identify request and caches the result.
func (h *Harvest) identify(ctx context.Context) error {
	req := oai.Request{
		Verb:         "Identify",
		BaseURL:      h.Config.BaseURL,
		ExtraHeaders: h.Config.ExtraHeaders,
	}
	if h.Client == nil {
		h.Client = oai.DefaultClient
	}
	resp, err := h.Client.DoContext(ctx, &req)
	if err != nil {
		if !encodingSuspect(err) {
			return err
		}
		log.Printf("trying workaround: %v", err)
		// try to workaround for the whole harvest
		if h.Config.ExtraHeaders == nil {
			h.Config.ExtraHeaders = make(http.Header)
		}
		h.Config.ExtraHeaders.Set("Accept-Encoding", "identity")
		// also apply to this request
		req.ExtraHeaders = h.Config.ExtraHeaders
		resp, err = h.Client.DoContext(ctx, &req)
		if err != nil {
			return err
		}
	}
	// A URL that is not an endpoint answers something - a home page, a login
	// form, a 200 carrying an error - and the lenient decoder turns all of them
	// into an Identify with nothing in it. Catching that here is what keeps the
	// mistake off the disk: NewHarvest runs before a writer is opened, so a
	// mistyped URL leaves no shard for metha stat to list afterwards.
	if resp.Identify.IsEmpty() {
		return fmt.Errorf("%w: %s", ErrNotAnEndpoint, h.Config.BaseURL)
	}
	h.Identify = &resp.Identify
	return nil
}
