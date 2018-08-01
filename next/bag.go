// Package bag contains a refactored version of the harvesting logic.
package bag

import (
	"crypto/sha1"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/miku/metha"
)

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
func (h *Harvest) Identify() (*Identify, error) {
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
func (h *Harvest) MustIdentify() *Identify {
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
	return filepath.Join(BaseDir, fmt.Sprintf("%x", h.Sum(nil)))
}
