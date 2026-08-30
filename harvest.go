package metha

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
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
	// ErrInvalidEarliestDate for unparsable earliest date.
	ErrInvalidEarliestDate = errors.New("invalid earliest date")
	// ErrNoSink is returned by Run when there is nowhere to write.
	ErrNoSink = errors.New("harvest needs a sink")
)

// Sink receives the responses of a harvest, one window at a time, and is the
// only way a harvest writes anything.
//
// A window opens with Begin, takes one Append per response and becomes durable
// at Commit, or is discarded by Abort. HasWindow and Resume answer what has
// already been harvested. The interface exists because this package cannot
// import store, whose Writer is the only implementation outside a test.
type Sink interface {
	Begin(from, until time.Time, settled bool) error
	Append(raw []byte) error
	Commit() error
	Abort(cause error) error
	HasWindow(from, until time.Time) (bool, error)
	Resume() (time.Time, error)
}

// PrependSchema prepends http, if its missing.
func PrependSchema(s string) string {
	if !strings.HasPrefix(s, "http") {
		return fmt.Sprintf("http://%s", s)
	}
	return s
}

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
	Client *Client

	// Sink is where the harvested responses go. The caller owns it, and closes
	// it.
	Sink Sink

	// XXX: Lazy via sync.Once?
	Identify *Identify
	Started  time.Time
	// Protects the work a termination signal must not land in the middle of:
	// every call into the sink, which the signal handler closes. See sinkBegin.
	sync.Mutex
}

// NewHarvest creates a new harvest. A network connection will be used for an initial Identify request.
func NewHarvest(baseURL string) (*Harvest, error) {
	h := Harvest{Config: &Config{
		BaseURL:      baseURL,
		MaxRetries:   3,
		RetryDelay:   10 * time.Second,
		RetryBackoff: 2.0,
	}}
	if err := h.identify(); err != nil {
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

// Run starts the harvest. The sink owns the directory and the lock, so there is
// nothing to prepare here beyond the clock and the signal handler.
func (h *Harvest) Run() error {
	if h.Sink == nil {
		return ErrNoSink
	}
	h.setupInterruptHandler()
	h.Started = time.Now()
	return h.run()
}

// setupInterruptHandler will cleanup, so we can CTRL-C or kill savely.
func (h *Harvest) setupInterruptHandler() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM) // SIGTERM for systemd stop, etc.
	go func() {
		<-sigc
		log.Println("waiting for the window in flight to finish...")
		// Taken and never given back: the process is going away, and whatever is
		// waiting on it must not wake up to a closed sink. This is why the
		// shutdown cannot be left to a defer.
		h.Lock()
		h.shutdown()
		os.Exit(0)
	}()
}

// shutdown closes the sink, with h already locked. Split out of the handler so
// that it can be exercised without a signal, which would take the test binary
// with it. Closing the sink drops the window in flight and releases its lock.
func (h *Harvest) shutdown() {
	if c, ok := h.Sink.(io.Closer); ok {
		if err := c.Close(); err != nil {
			log.Printf("closing sink: %v", err)
		}
	}
}

// sinkBegin and the calls below it hold the harvest mutex because the signal
// handler runs on its own goroutine and closes the sink, and it can arrive at
// any point in a window. Going through the mutex
// puts the close between two calls rather than inside one, where it would be
// closing a writer with an open transaction. Between two calls it only drops
// the window in flight, which is the crash-recovery path the writer already
// has - the torn tail is truncated on the next open, so the cost is a window,
// not a shard.
func (h *Harvest) sinkBegin(from, until time.Time, settled bool) error {
	h.Lock()
	defer h.Unlock()
	return h.Sink.Begin(from, until, settled)
}

func (h *Harvest) sinkAppend(raw []byte) error {
	h.Lock()
	defer h.Unlock()
	return h.Sink.Append(raw)
}

func (h *Harvest) sinkCommit() error {
	h.Lock()
	defer h.Unlock()
	return h.Sink.Commit()
}

func (h *Harvest) sinkAbort(cause error) error {
	h.Lock()
	defer h.Unlock()
	return h.Sink.Abort(cause)
}

func (h *Harvest) sinkResume() (time.Time, error) {
	h.Lock()
	defer h.Unlock()
	return h.Sink.Resume()
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

// coverage asks the sink how far it got. It is the one question the plan puts to
// the disk, and the reason the plan itself needs none: a store that can tell a
// settled window from one holding only what existed at the moment of asking
// hands back the start of the latter, so its range is covered again rather than
// resumed past.
func (h *Harvest) coverage() (Coverage, error) {
	resume, err := h.sinkResume()
	return Coverage{Resume: resume}, err
}

// retry attempts an operation with exponential backoff
func (h *Harvest) retry(operation func() (*Response, error)) (*Response, error) {
	var lastErr error
	delay := h.Config.RetryDelay
	for attempt := 0; attempt <= h.Config.MaxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("retry attempt %d/%d after %v", attempt, h.Config.MaxRetries, delay)
			time.Sleep(delay)
			// Apply backoff for next attempt
			delay = time.Duration(float64(delay) * h.Config.RetryBackoff)
		}
		resp, err := operation()
		if err == nil {
			return resp, nil
		}
		// Save the error for potential return
		lastErr = err
		// Check if we should retry based on the error
		if !h.shouldRetry(err) {
			return nil, err
		}
		log.Printf("request failed (attempt %d/%d): %v", attempt+1, h.Config.MaxRetries, err)
	}
	return nil, fmt.Errorf("failed after %d retries: %w", h.Config.MaxRetries, lastErr)
}

// shouldRetry determines if an error should trigger a retry
func (h *Harvest) shouldRetry(err error) bool {
	// Don't retry if we're not configured to handle HTTP errors
	if !h.Config.IgnoreHTTPErrors {
		return false
	}
	// Check for specific HTTP errors that we want to retry
	if httpErr, ok := err.(HTTPError); ok {
		switch httpErr.StatusCode {
		case 408, // Request Timeout
			429, // Too Many Requests
			500, // Internal Server Error
			502, // Bad Gateway
			503, // Service Unavailable
			504: // Gateway Timeout
			return true
		}
	}
	// Check for network-related errors
	if errors.Is(err, io.ErrUnexpectedEOF) ||
		strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "no such host") ||
		strings.Contains(err.Error(), "timeout") {
		return true
	}
	return false
}

// run plans the harvest and works through it, one window at a time.
func (h *Harvest) run() (err error) {
	cfg := h.planConfig()
	var cov Coverage
	if !cfg.Unbounded {
		// An unbounded harvest covers whatever the endpoint chooses to send and
		// resumes from nothing, so it never asks.
		if cov, err = h.coverage(); err != nil {
			return err
		}
	}
	windows, err := Plan(cov, h.Identify, h.Started, cfg)
	if err != nil {
		return fmt.Errorf("failed to plan harvest: %w", err)
	}
	if n := len(windows); n > 0 && !windows[0].Boundless() {
		log.Printf("plan: %d window(s), %v to %v", n, windows[0].Begin, windows[n-1].End)
	}

	for _, w := range windows {
		if err := h.runWindow(w); err != nil {
			if h.Config.IgnoreUnexpectedEOF && errors.Is(err, io.ErrUnexpectedEOF) {
				log.Printf("ignoring unexpected EOF and moving to the next window")
				continue
			}
			return err
		}
	}
	return nil
}

// runWindow fetches one window of the plan: one request plus every resumption
// token that follows from it.
func (h *Harvest) runWindow(w Window) (err error) {
	// A boundless window claims no range at all. Its bytes still accumulate, the
	// blob layer being append-only, but only the newest copy is indexed and so
	// only that one is ever read.
	if err := h.sinkBegin(w.Begin, w.End, w.Settled); err != nil {
		return err
	}
	defer func() {
		// A window that did not reach Commit leaves nothing behind.
		if err != nil {
			err = errors.Join(err, h.sinkAbort(err))
		}
	}()
	var token string
	var i, empty int
	for {
		if h.Config.MaxRequests == i {
			log.Printf("max requests limit (%d) reached", h.Config.MaxRequests)
			break
		}
		req := Request{
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

		if h.Config.Delay > 0 {
			time.Sleep(h.Config.Delay)
		}

		// Use retry mechanism for the request
		resp, err := h.retry(func() (*Response, error) {
			return h.Client.Do(&req)
		})

		if err != nil {
			// If we've exhausted all retries and still have an error
			if !h.Config.IgnoreHTTPErrors {
				return fmt.Errorf("failed to make request after retries: %w", err)
			}
			// If we're ignoring HTTP errors, continue to next iteration
			i++
			continue
		}

		// Handle OAI specific errors. XXX: An badResumptionToken kind of error
		// might be recoverable, by simply restarting the harvest.
		if resp.Error.Code != "" {
			// Rare case, where a resumptionToken is given, but it leads to
			// noRecordsMatch - we still want to save, whatever we got up until
			// this point, so we break here.
			switch resp.Error.Code {
			case "noRecordsMatch":
				if !resp.HasResumptionToken() {
					break
				}
				log.Println("resumptionToken set and noRecordsMatch, continuing")
			case "badResumptionToken":
				log.Println("badResumptionToken, might signal end-of-harvest")
			case "InternalException":
				// #9717, InternalException Could not send Message.
				log.Println("InternalException: retrying request in a few instants...")
				time.Sleep(30 * time.Second)
				i++ // Count towards the total request limit.
				continue
			default:
				return resp.Error
			}
		}
		b, err := xml.Marshal(resp)
		if err != nil {
			return err
		}
		if err := h.sinkAppend(b); err != nil {
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
		if empty == h.Config.MaxEmptyResponses {
			log.Printf("max number of empty responses reached")
			break
		}
	}
	return h.sinkCommit()
}

// identify runs an OAI identify request and caches the result.
func (h *Harvest) identify() error {
	req := Request{
		Verb:         "Identify",
		BaseURL:      h.Config.BaseURL,
		ExtraHeaders: h.Config.ExtraHeaders,
	}
	if h.Client == nil {
		h.Client = DefaultClient
	}
	resp, err := h.Client.Do(&req)
	if err != nil {
		log.Printf("trying workaround: %v", err)
		// try to workaround for the whole harvest
		if h.Config.ExtraHeaders == nil {
			h.Config.ExtraHeaders = make(http.Header)
		}
		h.Config.ExtraHeaders.Set("Accept-Encoding", "identity")
		// also apply to this request
		req.ExtraHeaders = h.Config.ExtraHeaders
		resp, err = h.Client.Do(&req)
		if err != nil {
			return err
		}
	}
	h.Identify = &resp.Identify
	return nil
}
