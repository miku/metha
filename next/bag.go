// Package bag contains a refactored version of the harvesting logic.
package bag

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
var BaseDir = filepath.Join(metha.UserHomeDir(), ".methax")

type Harvest struct {
	BaseURL string `json:"baseURL"`
	From    string `json:"-"`
	Until   string `json:"-"`
	Format  string `json:"format"`
	Set     string `json:"set"`

	Options *HarvestOptions `json:"-"`
	cache   struct {
		identify *metha.Identify
	}
}

type HarvestOptions struct {
	MaxRequest                 int
	DisableSelectiveHarvesting bool
	CleanBeforeDecode          bool
	IgnoreHTTPErrors           bool
	MaxEmptyResponses          int
	SuppressFormatParameter    bool
}

// Identify returns the result of an OAI identify request, possibly cached.
func (h *Harvest) Identify() (*metha.Identify, error) {
	if h.cache.identify == nil {
		req := metha.Request{Verb: "Identify", BaseURL: h.BaseURL}
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
	io.WriteString(hash, h.BaseURL)
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
func (h *Harvest) GranularityToLayout() string {
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

// XXX(miku): Find last successful date, choose an interval (option, force by
// error), attempt download (retry, hoops, as one way out of errors, restart
// with a smaller window size).
func (h *Harvest) Run() error {
	return nil
}
