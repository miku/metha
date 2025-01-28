package metha

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
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

var (
	// BaseDir is where all data is stored.
	BaseDir   = filepath.Join(UserHomeDir(), ".cache", "metha")
	fnPattern = regexp.MustCompile("(?P<Date>[0-9]{4,4}-[0-9]{2,2}-[0-9]{2,2})-[0-9]{8,}.xml(.gz)?$")

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

	// XXX: Lazy via sync.Once?
	Identify *Identify
	Started  time.Time
	// Protects the rare case, where we are in the process of renaming
	// harvested files and get a termination signal at the same time.
	sync.Mutex
}

// NewHarvest creates a new harvest. A network connection will be used for an initial Identify request.
func NewHarvest(baseURL string) (*Harvest, error) {
	h := Harvest{Config: &Config{BaseURL: baseURL}}
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
	return MustGlob(filepath.Join(h.Dir(), "*.xml.gz"))
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

// dateLayout converts the repository endpoints advertised granularity to Go
// date format strings.
func (h *Harvest) dateLayout() string {
	switch h.Identify.Granularity {
	case "YYYY-MM-DD":
		return "2006-01-02"
	case "YYYY-MM-DDThh:mm:ssZ":
		return "2006-01-02T15:04:05Z"
	}
	return ""
}

// Run starts the harvest.
func (h *Harvest) Run() error {
	if err := h.mkdirAll(); err != nil {
		return err
	}
	h.setupInterruptHandler()
	h.Started = time.Now()
	return h.run()
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
	signal.Notify(sigc, os.Interrupt)
	go func() {
		<-sigc
		log.Println("waiting for any rename to finish...")
		h.Lock()
		defer h.Unlock()
		if err := h.cleanupTemporaryFiles(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
}

// finalize will move all files with a given suffix into place.
func (h *Harvest) finalize(suffix string) error {
	var renamed []string

	h.Lock()
	defer h.Unlock()

	for _, src := range h.temporaryFilesSuffix(suffix) {
		dst := fmt.Sprintf("%s.gz", strings.Replace(src, suffix, "", -1))
		var err error
		if err = MoveCompressFile(src, dst); err == nil {
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
	if h.Config.From == "" {
		earliestDate, err = h.earliestDate()
	} else {
		earliestDate, err = time.Parse("2006-01-02", h.Config.From)
	}
	if err != nil {
		return Interval{}, err
	}

	// Last value for this directory.
	laster := DirLaster{
		Dir:          h.Dir(),
		DefaultValue: earliestDate.Format("2006-01-02"),
		ExtractorFunc: func(fi os.FileInfo) string {
			groups := fnPattern.FindStringSubmatch(fi.Name())
			if len(groups) > 1 {
				return groups[1]
			}
			return ""
		},
	}

	last, err := laster.Last()
	if err != nil {
		return Interval{}, err
	}

	begin, err := time.Parse("2006-01-02", last)
	if err != nil {
		return Interval{}, err
	}

	if last != laster.DefaultValue {
		// Add a single day, only if we are not just starting.
		begin = begin.AddDate(0, 0, 1)
	}

	var end time.Time
	if h.Config.Until != "" {
		end, err = time.Parse("2006-01-02", h.Config.Until)
		if err != nil {
			return Interval{}, err
		}
		log.Printf("using custom end date: %v", end)
	} else {
		end = now.New(h.Started.AddDate(0, 0, -1)).EndOfDay()
	}

	if last == end.Format("2006-01-02") {
		return Interval{}, ErrAlreadySynced
	}
	return Interval{Begin: begin, End: end}, nil
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

	var intervals []Interval

	switch {
	case h.Config.HourlyInterval:
		intervals = interval.HourlyIntervals()
	case h.Config.DailyInterval:
		intervals = interval.DailyIntervals()
	default:
		intervals = interval.MonthlyIntervals()
	}

	for _, iv := range intervals {
		if err := h.runInterval(iv); err != nil {
			if h.Config.IgnoreUnexpectedEOF && err == io.ErrUnexpectedEOF {
				log.Printf("ignoring unexpected EOF and moving to next interval")
				continue
			}
			return err
		}
	}
	return nil
}

// runInterval runs a selective harvest on the given interval.
func (h *Harvest) runInterval(iv Interval) error {
	suffix := fmt.Sprintf("-tmp-%d", rand.Intn(999999999))
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
			req.From = iv.Begin.Format(h.dateLayout())
			req.Until = iv.End.Format(h.dateLayout())
		}

		if h.Config.Delay > 0 {
			time.Sleep(h.Config.Delay)
		}
		// Do request, return any http error, except when we ignore HTTPErrors - in that case, break out early.
		resp, err := h.Client.Do(&req)
		if err != nil {
			if e, ok := err.(HTTPError); ok {
				if e.StatusCode == 422 {
					// https://github.com/miku/metha/issues/39
					// https://zenodo.org/oai2d?from=2014-02-03T00:00:00Z&metadataPrefix=marcxml&set=user-lory_phlu&until=2014-02-28T23:59:59Z&verb=ListRecords
					break
				}
			}
			if h.Config.IgnoreHTTPErrors {
				log.Printf("retrying an HTTP error (-ignore-http-errors): %v", err)
				time.Sleep(30 * time.Second)
				i++ // Count towards the total request limit.
				continue
			}
			return err
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
		// The filename consists of the right boundary (until), the
		// serial number of the request and a suffix, marking this
		// request in progress.
		filename := filepath.Join(h.Dir(), fmt.Sprintf("%s-%08d.xml%s", filedate, i, suffix))
		if b, err := xml.Marshal(resp); err == nil {
			if e := ioutil.WriteFile(filename, b, 0644); e != nil {
				return e
			}
			log.Printf("wrote %s", filename)
		} else {
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
	return h.finalize(suffix)
}

// earliestDate returns the earliest date as a time.Time value.
func (h *Harvest) earliestDate() (time.Time, error) {
	// Different granularities are possible: https://eudml.org/oai/OAIHandler?verb=Identify
	// First occurence of a non-standard granularity: https://t3.digizeitschriften.de/oai2/
	switch strings.ToLower(h.Identify.Granularity) {
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
