package metha

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jinzhu/now"
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
	// BaseDir is where all data is stored.
	BaseDir   = filepath.Join(UserHomeDir(), ".cache", "metha")
	fnPattern = regexp.MustCompile("(?P<Date>[0-9]{4,4}-[0-9]{2,2}-[0-9]{2,2})-[0-9]{8,}.xml(.gz|.zst)?$")

	// ErrAlreadySynced signals completion.
	ErrAlreadySynced = errors.New("already synced")
	// ErrInvalidEarliestDate for unparsable earliest date.
	ErrInvalidEarliestDate = errors.New("invalid earliest date")
)

type Harvester interface {
	Run() error
	Files() []string
	Dir() string
}

// Sink receives the responses of a harvest, one window at a time.
//
// A window opens with Begin, takes one Append per response and becomes durable
// at Commit, or is discarded by Abort. HasWindow and Resume answer what has
// already been harvested - the questions the file layout answers with a readdir
// over its filenames.
type Sink interface {
	Begin(from, until time.Time, settled bool) error
	Append(raw []byte) error
	Commit() error
	Abort(cause error) error
	HasWindow(from, until time.Time) (bool, error)
	Resume() (time.Time, error)
}

type CompressionType int

const (
	CompZstd CompressionType = iota
	CompGzip
)

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
	KeepTemporaryFiles         bool
	IgnoreUnexpectedEOF        bool
	Delay                      time.Duration
	MaxRetries                 int           // Maximum number of retry attempts
	RetryDelay                 time.Duration // Delay between retries
	RetryBackoff               float64       // Multiplier for delay between retries (e.g., 2.0 for exponential backoff)
	CompressionType            CompressionType
	CompressionLevel           int // -5 to 22 for zstd
	NoCompression              bool
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

	// Sink, when set, receives the harvested responses instead of the
	// directory of files. The caller owns it, and closes it.
	Sink Sink

	// XXX: Lazy via sync.Once?
	Identify *Identify
	Started  time.Time
	// Protects the rare case, where we are in the process of renaming
	// harvested files and get a termination signal at the same time.
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

// Dir returns the absolute path to the harvesting directory.
func (h *Harvest) Dir() string {
	data := []byte(h.Config.Set + "#" + h.Config.Format + "#" + h.Config.BaseURL)
	return filepath.Join(BaseDir, base64.RawURLEncoding.EncodeToString(data))
}

// Files returns all files for a given harvest, without the temporary files.
func (h *Harvest) Files() []string {
	xmlFiles := MustGlob(filepath.Join(h.Dir(), "*.xml"))
	gzipFiles := MustGlob(filepath.Join(h.Dir(), "*.xml.gz"))
	zstdFiles := MustGlob(filepath.Join(h.Dir(), "*.xml.zst"))
	files := append(xmlFiles, gzipFiles...)
	return append(files, zstdFiles...)
}

// mkdirAll creates necessary directories.
func (h *Harvest) mkdirAll() error {
	if _, err := os.Stat(h.Dir()); os.IsNotExist(err) {
		if err := os.MkdirAll(h.Dir(), 0755); err != nil {
			return err
		}
	}
	return nil
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
	case h.secondGranularity():
		return t.UTC().Format(utcSecond)
	case h.dayGranularity():
		return t.Format("2006-01-02")
	}
	return ""
}

// Helper to add the appropriate extension based on compression type
func compressedFilename(base string, compressionType CompressionType) string {
	switch compressionType {
	case CompZstd:
		return base + ".zst"
	default:
		return base + ".gz"
	}
}

// Run starts the harvest.
func (h *Harvest) Run() error {
	// A sink brings its own directory and its own lock; the file layout's
	// directory must not be created for a harvest that will not write in it.
	if h.Sink == nil {
		if err := h.mkdirAll(); err != nil {
			return err
		}
		unlock, err := h.lock()
		if err != nil {
			return err
		}
		defer unlock()
	}
	h.setupInterruptHandler()
	h.Started = time.Now()
	return h.run()
}

// lock takes the per-directory harvest lock, so that two processes cannot
// harvest the same endpoint into the same directory at once - which would
// interleave two sets of temporary files and finalize each other's data into
// place, quietly duplicating records. Returns an error wrapping ErrLocked if
// another harvest is already running here.
func (h *Harvest) lock() (unlock func(), err error) {
	f, err := TryFlock(filepath.Join(h.Dir(), LockName))
	if err != nil {
		return nil, err
	}
	if f == nil {
		return func() {}, nil // no flock on this platform
	}
	return func() { f.Close() }, nil
}

// temporaryFiles lists all temporary files in the harvesting dir.
func (h *Harvest) temporaryFiles() []string {
	return MustGlob(filepath.Join(h.Dir(), "*.xml-tmp*"))
}

// temporaryFilesSuffix list all temporary files in the harvesting dir having a suffix.
func (h *Harvest) temporaryFilesSuffix(suffix string) []string {
	return MustGlob(filepath.Join(h.Dir(), fmt.Sprintf("*.xml%s", suffix)))
}

// cleanupTemporaryFiles will remove all temporary files in the harvesting dir.
func (h *Harvest) cleanupTemporaryFiles() error {
	if h.Config.KeepTemporaryFiles {
		log.Printf("keeping %d temporary file(s) under %s",
			len(h.temporaryFiles()), h.Dir())
		return nil
	}
	for _, filename := range h.temporaryFiles() {
		if err := os.Remove(filename); err != nil {
			if e, ok := err.(*os.PathError); ok && e.Err == syscall.ENOENT {
				continue
			}
			return err
		}
	}
	return nil
}

// setupInterruptHandler will cleanup, so we can CTRL-C or kill savely.
func (h *Harvest) setupInterruptHandler() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM) // SIGTERM for systemd stop, etc.
	go func() {
		<-sigc
		log.Println("waiting for any rename to finish...")
		h.Lock()
		defer h.Unlock()
		// Closing the sink drops the window in flight and releases its lock;
		// nothing below this runs, so it cannot be left to a defer.
		if c, ok := h.Sink.(io.Closer); ok {
			if err := c.Close(); err != nil {
				log.Printf("closing sink: %v", err)
			}
		}
		if err := h.cleanupTemporaryFiles(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
}

func (h *Harvest) compressedFileExt() string {
	switch h.Config.CompressionType {
	case CompGzip:
		return "gz"
	case CompZstd:
		return "zst"
	default:
		return "zst"
	}
}

// finalFileName replace func compressedFileExt
func (h *Harvest) finalFileName(src, suffix string) string {
	base := strings.Replace(src, suffix, "", -1)
	if h.Config.NoCompression {
		return base
	}
	switch h.Config.CompressionType {
	case CompGzip:
		return base + ".gz"
	case CompZstd:
		return base + ".zst"
	default:
		return base + ".zst"
	}
}

// finalize will move all files with a given suffix into place.
func (h *Harvest) finalize(suffix string) error {
	var renamed []string

	h.Lock()
	defer h.Unlock()

	for _, src := range h.temporaryFilesSuffix(suffix) {
		//		dst := fmt.Sprintf("%s.%s", strings.Replace(src, suffix, "", -1), h.compressedFileExt())
		dst := h.finalFileName(src, suffix)
		var err error
		if h.Config.NoCompression {
			err = os.Rename(src, dst)
		} else {
			err = MoveCompressFile(src, dst, h.Config.CompressionType, h.Config.CompressionLevel)
		}
		if err == nil {
			renamed = append(renamed, dst)
			continue
		}
		// Try to cleanup all the already renamed files.
		for _, fn := range renamed {
			if e := os.Remove(fn); err != nil {
				if ee, ok := err.(*os.PathError); ok && ee.Err == syscall.ENOENT {
					continue
				}
				return &MultiError{[]error{
					err,
					e,
					fmt.Errorf("inconsistent cache state; start over and purge %s", h.Dir())},
				}
			}
		}
		return err
	}
	if len(renamed) > 0 {
		log.Printf("moved %d file(s) into place", len(renamed))
	}
	return nil
}

// defaultInterval returns a harvesting interval based on the cached
// state or earliest date, if this endpoint was not harvested before.
// If the harvest already has a From value set, we use it as earliest date.
func (h *Harvest) defaultInterval() (Interval, error) {
	var earliestDate time.Time
	var err error

	// refs #9100
	// Dates given as dates are read in the local zone, the one the window
	// boundaries below are computed in, so that the two can be compared.
	if h.Config.From == "" {
		earliestDate, err = h.earliestDate()
	} else {
		earliestDate, err = time.ParseInLocation("2006-01-02", h.Config.From, time.Local)
	}
	if err != nil {
		return Interval{}, err
	}

	begin, err := h.resumeFrom()
	if err != nil {
		return Interval{}, err
	}
	if begin.IsZero() {
		begin = earliestDate
	}

	var end time.Time
	if h.Config.Until != "" {
		until, err := time.ParseInLocation("2006-01-02", h.Config.Until, time.Local)
		if err != nil {
			return Interval{}, err
		}
		// A date-only bound means the whole of that day, which is how the
		// endpoint reads it; spelling it out keeps the window that gets
		// recorded honest, and stops a second granularity request from asking
		// for midnight and so missing the day it was given.
		end = now.New(until).EndOfDay()
		log.Printf("using custom end date: %v", end)
	} else {
		end = h.reachableEnd()
	}

	if begin.After(end) {
		return Interval{}, ErrAlreadySynced
	}
	return Interval{Begin: begin, End: end}, nil
}

// reachableEnd returns how far into the present a harvest can ask, which is as
// far as the endpoint's granularity lets a request reach. An endpoint that
// speaks only dates cannot be asked for less than a whole day, so the harvest
// takes the whole of today and records it as unsettled; see settledFrom.
func (h *Harvest) reachableEnd() time.Time {
	if h.Sink == nil {
		// The file layout remembers a window by a date in a filename and has
		// nowhere to note that the day was not over yet, so reaching into today
		// would strand the rest of it. It stops where it always did.
		return now.New(h.Started.AddDate(0, 0, -1)).EndOfDay()
	}
	if h.secondGranularity() {
		return h.Started.Truncate(time.Second)
	}
	return now.New(h.Started).EndOfDay()
}

// settledFrom returns the instant from which an endpoint's datestamps can still
// change, and so the point a harvest must come back to on its next run.
//
// The problem it answers: a window is remembered by the range it covered, and
// with daily granularity "until today" is the only thing a request can say. Ask
// it at noon and the answer holds the morning's records only - but the window
// claims the whole day, so a run tomorrow would resume past it and lose the
// afternoon for good. Anything at or after this point is therefore recorded as
// unsettled and fetched again.
//
// Truncated to the second, the finest an OAI request can express, which also
// keeps stored boundaries comparable as strings.
func (h *Harvest) settledFrom() time.Time {
	if h.secondGranularity() {
		// Datestamps are exact here, so only the recent past is in doubt: a
		// clock that runs behind ours, or a record indexed a moment after it
		// was stamped, would otherwise land just before a boundary we have
		// already passed.
		//
		// Truncated to whole lags rather than to the second, so that the
		// boundary stands still between runs the way BeginningOfDay does below.
		// A boundary that moved with the clock made every re-run split off a
		// settled window a few seconds wide - one request and one row each
		// time, for a sliver of time nothing happened in. Quantised, a re-run
		// inside the same lag asks the one question that is still open, and the
		// window it commits replaces the one before it.
		return h.Started.Add(-SettleLag).Truncate(SettleLag)
	}
	return now.New(h.Started).BeginningOfDay()
}

// secondGranularity reports whether the endpoint stamps records to the second.
// An endpoint that says nothing intelligible about its granularity - or that
// was never asked - is treated as the coarser of the two, which is the
// assumption that cannot lose records.
func (h *Harvest) secondGranularity() bool {
	return h.granularity() == "yyyy-mm-ddthh:mm:ssz"
}

// dayGranularity reports whether the endpoint stamps records to the day.
func (h *Harvest) dayGranularity() bool {
	return h.granularity() == "yyyy-mm-dd"
}

// granularity is the endpoint's advertised granularity, folded to lower case.
// The spec gives the two forms in a fixed case, but enough endpoints get that
// wrong that reading them literally would drop the bounds from every request;
// earliestDate has always compared them this way.
func (h *Harvest) granularity() string {
	if h.Identify == nil {
		return ""
	}
	return strings.ToLower(h.Identify.Granularity)
}

// resumeFrom returns the instant this harvest continues from, or the zero time
// if this endpoint was never harvested. A sink keeps the point explicitly and
// can point back at a window that is not settled yet; the file layout has only
// the dates in its filenames, where a date stands for the whole of that day.
func (h *Harvest) resumeFrom() (time.Time, error) {
	if h.Sink != nil {
		return h.Sink.Resume()
	}
	laster := DirLaster{
		Dir: h.Dir(),
		ExtractorFunc: func(dirent os.DirEntry) string {
			groups := fnPattern.FindStringSubmatch(dirent.Name())
			if len(groups) > 1 {
				return groups[1]
			}
			return ""
		},
	}
	last, err := laster.Last()
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil // Never harvested.
		}
		return time.Time{}, err
	}
	if last == "" {
		return time.Time{}, nil
	}
	// The date in the filename is the local date the window ended on, since
	// that is the zone its boundary was computed in.
	t, err := time.ParseInLocation("2006-01-02", last, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	return t.AddDate(0, 0, 1), nil
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

// run runs a harvest: one request plus subsequent tokens.
func (h *Harvest) run() (err error) {
	defer func() {
		if e := h.cleanupTemporaryFiles(); e != nil {
			if err != nil {
				err = &MultiError{[]error{err, e}}
			}
			err = e
		}
	}()

	if h.Config.DisableSelectiveHarvesting {
		return h.runInterval(Interval{})
	}

	interval, err := h.defaultInterval()
	if err != nil {
		return fmt.Errorf("failed to get default interval: %w", err)
	}

	// The settled part is cut into windows as it always was. What is left
	// reaches into the endpoint's still-changing present and stays one window,
	// so that the next run repeats exactly that range instead of a growing tail
	// of it: tomorrow the same split leaves today's window behind as a settled
	// one, whose range matches to the nanosecond and so replaces it.
	settled, unsettled := interval.SplitAt(h.settledFrom())

	var intervals []Interval

	switch {
	case h.Config.HourlyInterval:
		intervals = settled.HourlyIntervals()
	case h.Config.DailyInterval:
		intervals = settled.DailyIntervals()
	default:
		intervals = settled.MonthlyIntervals()
	}
	if !unsettled.Empty() {
		intervals = append(intervals, unsettled)
	}

	for _, iv := range intervals {
		if err := h.runInterval(iv); err != nil {
			if h.Config.IgnoreUnexpectedEOF && errors.Is(err, io.ErrUnexpectedEOF) {
				log.Printf("ignoring unexpected EOF and moving to next interval")
				continue
			}
			return err
		}
	}
	return nil
}

// runInterval runs a selective harvest on the given interval.
func (h *Harvest) runInterval(iv Interval) (err error) {
	suffix := fmt.Sprintf("-tmp-%d", rand.Intn(999999999))
	if h.Sink != nil {
		from, until := iv.Begin, iv.End
		if h.Config.DisableSelectiveHarvesting {
			// No range was requested, so the window claims none: it is the
			// whole repository as of now, and the zero time is how that is
			// spelled. Being the same claim on every run is the point - a
			// re-harvest replaces the window it already has instead of
			// stacking another copy of the endpoint beside it. The bytes do
			// still accumulate, since the blob layer is append-only, but only
			// the newest copy is indexed and so only that one is ever read.
			from, until = time.Time{}, time.Time{}
		}
		// A window is settled when it ends before the point where the
		// endpoint's datestamps can still change; anything else is fetched
		// again on the next run. A harvest that cannot say what it covered -
		// no range was requested - is never settled.
		settled := !h.Config.DisableSelectiveHarvesting && until.Before(h.settledFrom())
		if err := h.Sink.Begin(from, until, settled); err != nil {
			return err
		}
		defer func() {
			// A window that did not reach Commit leaves nothing behind, the
			// way an unrenamed temporary file did.
			if err != nil {
				err = errors.Join(err, h.Sink.Abort(err))
			}
		}()
	}
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
		var filedate string
		if h.Config.DisableSelectiveHarvesting {
			// Used, when endpoint cannot handle from and until.
			filedate = h.Started.Format("2006-01-02")
		} else {
			filedate = iv.End.Format("2006-01-02")
			req.From = h.formatBound(iv.Begin)
			req.Until = h.formatBound(iv.End)
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
		if h.Sink != nil {
			if err := h.Sink.Append(b); err != nil {
				return err
			}
		} else {
			// The filename consists of the right boundary (until), the
			// serial number of the request and a suffix, marking this
			// request in progress.
			filename := filepath.Join(h.Dir(), fmt.Sprintf("%s-%08d.xml%s", filedate, i, suffix))
			if e := os.WriteFile(filename, b, 0644); e != nil {
				return e
			}
			log.Printf("wrote %s", filename)
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
	if h.Sink != nil {
		return h.Sink.Commit()
	}
	return h.finalize(suffix)
}

// earliestDate returns the earliest date as a time.Time value.
func (h *Harvest) earliestDate() (time.Time, error) {
	// Different granularities are possible: https://eudml.org/oai/OAIHandler?verb=Identify
	// First occurence of a non-standard granularity: https://t3.digizeitschriften.de/oai2/
	switch h.granularity() {
	case "yyyy-mm-dd":
		if len(h.Identify.EarliestDatestamp) <= 10 {
			return time.Parse("2006-01-02", h.Identify.EarliestDatestamp)
		}
		return time.Parse("2006-01-02", h.Identify.EarliestDatestamp[:10])
	case "yyyy-mm-ddthh:mm:ssz":
		// refs. #8825
		if len(h.Identify.EarliestDatestamp) >= 10 && len(h.Identify.EarliestDatestamp) < 20 {
			return time.Parse("2006-01-02", h.Identify.EarliestDatestamp[:10])
		}
		return time.Parse("2006-01-02T15:04:05Z", h.Identify.EarliestDatestamp)
	default:
		return time.Time{}, ErrInvalidEarliestDate
	}
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

// init takes configuration from the environment, if there is any.
func init() {
	if dir := os.Getenv("METHA_DIR"); dir != "" {
		BaseDir = dir
	}
}
