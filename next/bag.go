// Package bag contains a refactored version of the harvesting logic.
package next

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
)

// BaseDir for harvests, XXX(miku): use env, globalconf or similar.
var BaseDir = filepath.Join(metha.UserHomeDir(), ".metha-next")

var ErrNoMoreUpdates = errors.New("no more updates")

// Harvest contains the basic information on the harvest. Additionally some
// options.
type Harvest struct {
	Endpoint string
	From     string
	Until    string
	Format   string
	Set      string

	Options *Options
	cache   struct {
		identify *metha.Identify
	}

	UpdatedAt time.Time
}

// Options groups options.
type Options struct {
	MaxRequest                 int
	DisableSelectiveHarvesting bool
	CleanBeforeDecode          bool
	IgnoreHTTPErrors           bool
	MaxEmptyResponses          int
	SuppressFormatParameter    bool
	Window                     string
}

// Description describes a harvest, and some metadata. This will be serialized
// into a file. It is required, because we do not want a full database, but
// also do not want to put down all information in the names. The Description
// does not contain any historical facts, it should be recreatable from a
// harvest value and filesystem state alone.
type Description struct {
	Endpoint  string    `json:"endpoint"`
	Format    string    `json:"format"`
	Set       string    `json:"set"`
	Files     []string  `json:"files"`
	UpdatedAt time.Time `json:"updated"`
}

// Identify returns the result of an OAI identify request, possibly cached.
func (h *Harvest) Identify() (*metha.Identify, error) {
	if h.cache.identify == nil {
		req := metha.Request{Verb: "Identify", BaseURL: h.Endpoint}
		client := metha.CreateClient(30*time.Second, 2)
		resp, err := client.Do(&req)
		if err != nil {
			return nil, err
		}
		h.cache.identify = &resp.Identify
	}
	return h.cache.identify, nil
}

// MustIdentify panic, if the request cannot be made.
func (h *Harvest) MustIdentify() *metha.Identify {
	r, err := h.Identify()
	if err != nil {
		panic(err)
	}
	return r
}

// Description returns the description of this harvest. It loads it from a
// fixed file in the harvesting directory or otherwise creates a minimal
// object.
func (h *Harvest) Description() (*Description, error) {
	if _, err := os.Stat(h.descriptionPath()); os.IsNotExist(err) {
		return &Description{
			Endpoint:  h.Endpoint,
			Format:    h.Format,
			Set:       h.Set,
			UpdatedAt: time.Now(),
		}, nil
	}
	f, err := os.Open(h.descriptionPath())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var desc Description
	if err := json.NewDecoder(f).Decode(&desc); err != nil {
		return nil, err
	}
	return &desc, nil
}

// writeDescription persist a description of the harvest to a file.
func (h *Harvest) writeDescription() (err error) {
	if err = h.mkdirAll(); err != nil {
		return err
	}
	desc := Description{
		Endpoint:  h.Endpoint,
		Format:    h.Format,
		Set:       h.Set,
		UpdatedAt: time.Now(),
	}
	desc.Files, err = h.Files()
	if err != nil {
		return err
	}
	f, err := os.Create(h.descriptionPath())
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(desc); err != nil {
		return err
	}
	return nil
}

// Path to file, that describes this harvest briefly.
func (h *Harvest) descriptionPath() string {
	return filepath.Join(h.Dir(), "about.json")
}

// Directory of the harvest.
func (h *Harvest) Dir() string {
	hash := sha1.New()
	io.WriteString(hash, h.Endpoint)
	io.WriteString(hash, h.Format)
	io.WriteString(hash, h.Set)
	return filepath.Join(BaseDir, fmt.Sprintf("%x", hash.Sum(nil)))
}

// mkdirAll creates directories required for this harvest.
func (h *Harvest) mkdirAll() error {
	if _, err := os.Stat(h.Dir()); os.IsNotExist(err) {
		if err := os.MkdirAll(h.Dir(), 0755); err != nil {
			return err
		}
	}
	return nil
}

// GranularityToLayout converts the advertised granularity to Go date layout
// strings. There are two valid values, all else are errors. Granularity
// controls the kind of request windows possible.
func (h *Harvest) GranularityToLayout() (string, error) {
	idfy, err := h.Identify()
	if err != nil {
		return "", err
	}
	switch idfy.Granularity {
	case "YYYY-MM-DD":
		return "2006-01-02", nil
	case "YYYY-MM-DDThh:mm:ssZ":
		return "2006-01-02T15:04:05Z", nil
	default:
		return "", fmt.Errorf("invalid or missing granularity: %s", idfy.Granularity)
	}
}

// Files returns a list of harvested files, which have a common prefix.
func (h *Harvest) Files() (files []string, err error) {
	fis, err := ioutil.ReadDir(h.Dir())
	if err != nil {
		return
	}
	for _, f := range fis {
		if f.Name() == "about.json" {
			continue
		}
		if !strings.HasPrefix(f.Name(), "slice-") {
			continue
		}
		files = append(files, f.Name())
	}
	return
}

// LatestFile that has been harvested.
func (h *Harvest) LatestFile() (string, error) {
	files, err := h.Files()
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	return files[0], nil
}

// XXX(miku): Find last successful date (load from about.json, fallback to
// filesystem), choose an interval (option, force by error), attempt download
// (retry, hoops, as one way out of errors, restart with a smaller window
// size).
func (h *Harvest) Run() error {
	defer h.writeDescription()
	if _, err := h.Identify(); err != nil {
		return err
	}
	if err := h.mkdirAll(); err != nil {
		return err
	}
	for {
		err := h.run()
		if err == ErrNoMoreUpdates {
			break
		}
		if err != nil {
			return err
		}
		log.Println("fetched ...")
	}
	return nil
}

// run runs the next request. Will signal no more updates available with an error value.
func (h *Harvest) run() error {
	// Get supported granularity as layout.
	layout, err := h.GranularityToLayout()
	if err != nil {
		return err
	}
	// Find last date.
	latest, err := h.LatestFile()
	if err != nil {
		return err
	}
	// Find the start of the next slice.
	var start time.Time

	switch {
	case latest == "":
		log.Println("no previous files found")
		// XXX: Timestamp can be falsely advertised.
		var err error
		if start, err = time.Parse(layout, h.MustIdentify().EarliestDatestamp); err != nil {
			return err
		}
		log.Printf("using %s as first date", start)
	default:
		// Parse latest date from filename (slice_start_end_serial.xml).
		parts := strings.Split(latest, "_")
		if len(parts) != 4 {
			return fmt.Errorf("invalid file pattern: %s", latest)
		}
		i, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid end date: %s", parts[2])
		}
		start = time.Unix(i, 0)
	}
	log.Printf("start of next slice: %s", start.Format(time.RFC3339))
	// Try various skips, from one month, to one day, to 30 minutes.
	// skips := []int64{2592000, 86400, 1800}

	// Depending on the current interval, request next slice.
	// Resumptiontokens.
	// Finalize files
	// Done.
	return ErrNoMoreUpdates
}
