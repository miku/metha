package metha

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
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

// PrependSchema prepends http, if its missing.
func PrependSchema(s string) string {
	if !strings.HasPrefix(s, "http") {
		return fmt.Sprintf("http://%s", s)
	}
	return s
}

// Harvest contains parameters for mass-download. MaxRequests and
// CleanBeforeDecode are switches to handle broken token implementations and
// funny chars in responses. Some repos do not support selective harvesting
// (e.g. zvdd.org/oai2). Set "DisableSelectiveHarvesting" to try to grab
// metadata from these repositories. From and Until must always be given with
// 2006-01-02 layout. TODO(miku): make zero type work (lazily run identify).
type Harvest struct {
	BaseURL string
	Format  string
	Set     string
	From    string
	Until   string

	// XXX: Factor these out into options.
	MaxRequests                int
	DisableSelectiveHarvesting bool
	CleanBeforeDecode          bool
	IgnoreHTTPErrors           bool
	MaxEmptyResponses          int
	SuppressFormatParameter    bool
	HourlyInterval             bool
	DailyInterval              bool
	ExtraHeaders               http.Header

	// XXX: Lazy via sync.Once?
	Identify *Identify
	Started  time.Time

	// Protects the rare case, where we are in the process of renaming
	// harvested files and get a termination signal at the same time.
	sync.Mutex
}

// NewHarvest creates a new harvest. A network connection will be used for an initial Identify request.
func NewHarvest(baseURL string) (*Harvest, error) {
	h := Harvest{BaseURL: baseURL}
	if err := h.identify(); err != nil {
		return nil, err
	}
	return &h, nil
}

// Dir returns the absolute path to the harvesting directory.
func (h *Harvest) Dir() string {
	data := []byte(h.Set + "#" + h.Format + "#" + h.BaseURL)
	return filepath.Join(BaseDir, base64.RawURLEncoding.EncodeToString(data))
}

// MkdirAll creates necessary directories.
func (h *Harvest) MkdirAll() error {
	if _, err := os.Stat(h.Dir()); os.IsNotExist(err) {
		if err := os.MkdirAll(h.Dir(), 0755); err != nil {
			return err
		}
	}
	return nil
}

// Files returns all files for a given harvest, without the temporary files.
func (h *Harvest) Files() []string {
	return MustGlob(filepath.Join(h.Dir(), "*.xml.gz"))
}

// DateLayout converts the repository endpoints advertised granularity to Go
// date format strings.
func (h *Harvest) DateLayout() string {
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
	if err := h.MkdirAll(); err != nil {
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
	signal.Notify(sigc, syscall.SIGINT)

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
	if h.From == "" {
		earliestDate, err = h.earliestDate()
	} else {
		earliestDate, err = time.Parse("2006-01-02", h.From)
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

	end := now.New(h.Started.AddDate(0, 0, -1)).EndOfDay()

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

	if h.DisableSelectiveHarvesting {
		return h.runInterval(Interval{})
	}

	interval, err := h.defaultInterval()
	if err != nil {
		return err
	}

	var intervals []Interval

	switch {
	case h.HourlyInterval:
		intervals = interval.HourlyIntervals()
	case h.DailyInterval:
		intervals = interval.DailyIntervals()
	default:
		intervals = interval.MonthlyIntervals()
	}

	for _, iv := range intervals {
		if err := h.runInterval(iv); err != nil {
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
		if h.MaxRequests == i {
			log.Printf("max requests limit (%d) reached", h.MaxRequests)
			break
		}

		req := Request{
			BaseURL:                 h.BaseURL,
			MetadataPrefix:          h.Format,
			Verb:                    "ListRecords",
			Set:                     h.Set,
			ResumptionToken:         token,
			CleanBeforeDecode:       h.CleanBeforeDecode,
			SuppressFormatParameter: h.SuppressFormatParameter,
			ExtraHeaders:            h.ExtraHeaders,
		}

		var filedate string

		if h.DisableSelectiveHarvesting {
			// Used, when endpoint cannot handle from and until.
			filedate = h.Started.Format("2006-01-02")
		} else {
			filedate = iv.End.Format("2006-01-02")
			req.From = iv.Begin.Format(h.DateLayout())
			req.Until = iv.End.Format(h.DateLayout())
		}

		// Do request, return any http error, except when we ignore HTTPErrors - in that case, break out early.
		resp, err := Do(&req)
		if err != nil {
			if h.IgnoreHTTPErrors {
				log.Printf("stopping early due to failed request (IgnoreHTTPErrors=true): %s", err)
				break
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
				break
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

		if token = resp.GetResumptionToken(); token == "" {
			break
		}

		i++

		if len(resp.ListRecords.Records) > 0 {
			empty = 0
		} else {
			empty++
			log.Printf("warning: successive empty response: %d/%d", empty, h.MaxEmptyResponses)
		}
		if empty == h.MaxEmptyResponses {
			log.Printf("max number of empty responses reached")
			break
		}
	}
	return h.finalize(suffix)
}

// earliestDate returns the earliest date as a time.Time value.
func (h *Harvest) earliestDate() (time.Time, error) {
	// Different granularities are possible: https://eudml.org/oai/OAIHandler?verb=Identify
	switch h.Identify.Granularity {
	case "YYYY-MM-DD":
		if len(h.Identify.EarliestDatestamp) <= 10 {
			return time.Parse("2006-01-02", h.Identify.EarliestDatestamp)
		}
		return time.Parse("2006-01-02", h.Identify.EarliestDatestamp[:10])
	case "YYYY-MM-DDThh:mm:ssZ":
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
		BaseURL:      h.BaseURL,
		ExtraHeaders: h.ExtraHeaders,
	}

	c := CreateClient(30*time.Second, 2)

	resp, err := c.Do(&req)
	if err != nil {
		return err
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
