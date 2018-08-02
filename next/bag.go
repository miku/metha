// Package bag contains a refactored version of the harvesting logic.
package next

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/miku/metha"
)

// BaseDir for harvests, XXX(miku): use env.
var BaseDir = filepath.Join(metha.UserHomeDir(), ".metha-next")

// Harvest contains the basic information on the harvest. Additionally some
// options.
type Harvest struct {
	Endpoint string
	From     string
	Until    string
	Format   string
	Set      string

	Options *HarvestOptions
	cache   struct {
		identify *metha.Identify
	}
}

// HarvestOptions groups options.
type HarvestOptions struct {
	MaxRequest                 int
	DisableSelectiveHarvesting bool
	CleanBeforeDecode          bool
	IgnoreHTTPErrors           bool
	MaxEmptyResponses          int
	SuppressFormatParameter    bool
}

// Descriptor describes a harvest. This will be serialized into a file.
type Descriptor struct {
	Endpoint  string    `json:"endpoint"`
	Format    string    `json:"format"`
	Set       string    `json:"set"`
	Files     []string  `json:"files"`
	CreatedAt time.Time `json:"created"`
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

// Path to file, that describes this harvest briefly.
func (h *Harvest) DescriptorPath() string {
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

// MkdirAll creates directories required for this harvest.
func (h *Harvest) MkdirAll() error {
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

// Write harvest description out. This replaces the awkward base64 encoding,
// which tied endpoint, format, set length to filesystem limits.
func (h *Harvest) WriteDescriptor() error {
	b, err := json.Marshal(h)
	if err != nil {
		return nil
	}
	return ioutil.WriteFile(h.DescriptorPath(), b, 0644)
}

// Files returns the absolute filenames that make up this harvest. XXX(miku):
// This will be expensive to run on each request.
func (h *Harvest) Files() []string {
	return nil
}

// lastWindow returns the left and right time boundary of the last successful
// window. Error, if there is none (new harvest).
func (h *Harvest) lastWindow() (left, right time.Time, err error) {
	return time.Time{}, time.Time{}, nil
}

// XXX(miku): Find last successful date (load from about.json, fallback to
// filesystem), choose an interval (option, force by error), attempt download
// (retry, hoops, as one way out of errors, restart with a smaller window
// size).
func (h *Harvest) Run() error {
	return nil
}
