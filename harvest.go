package metha

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jinzhu/now"
)

const Day = 24 * time.Hour

var (
	// BaseDir is where all downloaded data is stored
	BaseDir     = filepath.Join(UserHomeDir(), ".metha")
	fnPattern   = regexp.MustCompile("(?P<Date>[0-9]{4,4}-[0-9]{2,2}-[0-9]{2,2})-[0-9]{8,}.xml(.gz)?$")
	datePattern = regexp.MustCompile("[0-9]{4,4}-[0-9]{2,2}-[0-9]{2,2}")

	ErrAlreadySynced       = errors.New("already synced")
	ErrInvalidEarliestDate = errors.New("invalid earliest date")
)

// Prepends http, if missing.
func PrependSchema(s string) string {
	if !strings.HasPrefix(s, "http") {
		return fmt.Sprintf("http://%s", s)
	}
	return s
}

// Harvest contains parameters for a mass-download. MaxRequests and
// CleanBeforeDecode are switches to handle broken token implementations and
// funny chars in responses. Some repos do not support selective harvesting
// (e.g. zvdd.org/oai2). Set "DisableSelectiveHarvesting" to try to grab
// metadata from these repositories. Set "SkipBroken" to ignore errors. From and
// Until must always be given with 2006-01-02 layout.
type Harvest struct {
	BaseURL string
	Format  string
	Set     string
	From    string
	Until   string

	MaxRequests                int
	DisableSelectiveHarvesting bool
	CleanBeforeDecode          bool
	SkipBroken                 bool
	MaxEmptyResponses          int

	Identify *Identify
	Started  time.Time

	// protects the (rare) case, where we are in the process of renaming
	// harvested files and get a termination signal at the same time.
	sync.Mutex
}

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

// files returns all already harvested files (no temporary files).
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

func (h *Harvest) Run() error {
	if err := h.MkdirAll(); err != nil {
		return err
	}
	h.setupInterruptHandler()
	h.Started = time.Now()
	return h.run()
}

// temporaryFiles list all temporary files in the harvesting dir.
func (h *Harvest) temporaryFiles() []string {
	return MustGlob(filepath.Join(h.Dir(), "*.xml-tmp*"))
}

// temporaryFiles list all temporary files in the harvesting dir having a
// suffix.
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
		// allow h.finalize() to finish
		h.Lock()
		// for good measure
		defer h.Unlock()

		// cleanup anything left over
		if err := h.cleanupTemporaryFiles(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
}

func moveAndCompress(src, dst string) error {
	// compress files
	tmp := fmt.Sprintf("%s-tmp-%d", dst, rand.Intn(999999999))

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	ff, err := os.Open(src)
	if err != nil {
		return err
	}
	defer ff.Close()

	if _, err := io.Copy(gw, ff); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// finalize will move all files with a given suffix into place.
func (h *Harvest) finalize(suffix string) error {
	// collect all successfully renamed files
	var renamed []string

	// lock, so we can finish even in the presence of an term signal.
	h.Lock()
	defer h.Unlock()

	for _, filename := range h.temporaryFilesSuffix(suffix) {
		dst := fmt.Sprintf("%s.gz", strings.Replace(filename, suffix, "", -1))

		// if err := os.Rename(filename, dst); err != nil {
		if err := moveAndCompress(filename, dst); err != nil {
			// try to cleanup all the already renamed files
			for _, fn := range renamed {
				if e := os.Remove(fn); err != nil {
					if ee, ok := err.(*os.PathError); ok && ee.Err == syscall.ENOENT {
						continue
					}
					return &MultiError{[]error{err, e,
						fmt.Errorf("error during rename and subsequent cleanup: inconsistent cache state, start over and purge %s", h.Dir())}}
				}
			}
			// stop with an error, but still in a consistent state
			return err
		}
		renamed = append(renamed, dst)
	}
	if len(renamed) > 0 {
		log.Printf("moved %d files into place", len(renamed))
	}
	return nil
}

// run runs a harvest (one request plus subsequent tokens).
func (h *Harvest) run() (err error) {
	defer func() {
		// however we exit, cleanup any temporary files
		if e := h.cleanupTemporaryFiles(); e != nil {
			if err != nil {
				// we had a previous error and cleanup failed, too
				err = &MultiError{[]error{err, e}}
			}
			err = e
		}
	}()

	if h.DisableSelectiveHarvesting {
		return h.runInterval(Interval{})
	}

	// earliest date as default value
	earliestDate, err := h.earliestDate()
	if err != nil {
		return err
	}

	// last value for this directory
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
		return err
	}

	begin, err := time.Parse("2006-01-02", last)
	if err != nil {
		return err
	}

	if last != laster.DefaultValue {
		// add a single day, only if we are not just starting
		begin = begin.AddDate(0, 0, 1)
	}

	end := now.New(h.Started.AddDate(0, 0, -1)).EndOfDay()

	if last == end.Format("2006-01-02") {
		return ErrAlreadySynced
	}

	interval := Interval{Begin: begin, End: end}
	for _, iv := range interval.MonthlyIntervals() {
		if err := h.runInterval(iv); err != nil {
			return err
		}
	}
	return nil
}

func (h *Harvest) runInterval(iv Interval) error {
	// suffix for this batch
	suffix := fmt.Sprintf("-tmp-%d", rand.Intn(999999999))
	// current resumption token
	var token string
	// number of responses, empty responses
	var i, empty int

	for {
		req := Request{
			BaseURL:           h.BaseURL,
			MetadataPrefix:    h.Format,
			Verb:              "ListRecords",
			Set:               h.Set,
			ResumptionToken:   token,
			CleanBeforeDecode: h.CleanBeforeDecode,
		}

		var filedate string

		if h.DisableSelectiveHarvesting {
			// used, when endpoint cannot handle from and until
			filedate = h.Started.Format("2006-01-02")
		} else {
			filedate = iv.End.Format("2006-01-02")
			req.From = iv.Begin.Format(h.DateLayout())
			req.Until = iv.End.Format(h.DateLayout())
		}

		// do request, return any http error
		resp, err := Do(&req)
		if err != nil {
			return err
		}

		// handle OAI specific errors
		if resp.Error.Code != "" {
			// Rare case, where a resumptionToken is given, but it leads to noRecordsMatch, e.g. https://goo.gl/K3gpQB
			// we still want to save, whatever we got up until this point, so we break here.
			if resp.Error.Code == "noRecordsMatch" {
				if i > 0 {
					log.Println("possibly broken server implementation: resumptionToken leads to noRecordsMatch")
				}
				break
			}
		}

		// filename consists of the right boundary (until), the serial
		// number of the request and a suffix, marking this request in
		// progress
		filename := filepath.Join(h.Dir(), fmt.Sprintf("%s-%08d.xml%s", filedate, i, suffix))

		// write response to file
		if b, err := xml.Marshal(resp); err == nil {
			if e := ioutil.WriteFile(filename, b, 0644); e != nil {
				return e
			}
			log.Printf("written %s", filename)
		} else {
			return err
		}

		// the usual stop condition
		if token = resp.GetResumptionToken(); token == "" {
			break
		}

		i++

		// the other stop conditions
		if h.MaxRequests == i {
			log.Printf("max requests limit (%d) reached", h.MaxRequests)
			break
		}

		// stop, if we have too many empty responses, despite resumption tokens
		if len(resp.ListRecords.Records) > 0 {
			empty = 0
		} else {
			empty++
			log.Printf("warning: successive empty response: %d/%d", empty, h.MaxEmptyResponses)
		}
		if empty > h.MaxEmptyResponses {
			log.Printf("max number of empty responses reached")
			break
		}
	}
	// rename files
	if err := h.finalize(suffix); err != nil {
		return err
	}
	return nil
}

func (h *Harvest) earliestDate() (time.Time, error) {
	switch h.Identify.Granularity {
	case "YYYY-MM-DD":
		return time.Parse("2006-01-02", h.Identify.EarliestDatestamp)
	case "YYYY-MM-DDThh:mm:ssZ":
		return time.Parse("2006-01-02T15:04:05Z", h.Identify.EarliestDatestamp)
	default:
		return time.Time{}, ErrInvalidEarliestDate
	}
}

func (h *Harvest) identify() error {
	req := Request{Verb: "Identify", BaseURL: h.BaseURL}

	// use a less resilient client for indentify requests
	c := CreateClient(1*time.Second, 2)

	resp, err := c.Do(&req)
	if err != nil {
		return err
	}
	h.Identify = &resp.Identify
	return nil
}

func init() {
	if dir := os.Getenv("METHA_DIR"); dir != "" {
		BaseDir = dir
	}
}
