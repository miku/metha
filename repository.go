package metha

import (
	"encoding/base64"
	"io/ioutil"
	"strconv"
	"strings"
	"time"
)

// Repository represents an OAI endpoint.
type Repository struct {
	BaseURL string
}

// Formats returns a list of metadata formats.
func (r Repository) Formats() ([]MetadataFormat, error) {
	var formats []MetadataFormat
	var token string
	for {
		req := Request{BaseURL: r.BaseURL, Verb: "ListMetadataFormats", ResumptionToken: token}
		resp, err := Do(&req)
		if err != nil {
			return nil, err
		}
		formats = append(formats, resp.ListMetadataFormats.MetadataFormat...)
		if !resp.HasResumptionToken() {
			break
		}
		token = resp.GetResumptionToken()
	}
	return formats, nil
}

// Sets returns a list of sets.
func (r Repository) Sets() ([]Set, error) {
	var sets []Set
	var token string
	for {
		req := Request{BaseURL: r.BaseURL, Verb: "ListSets", ResumptionToken: token}
		resp, err := Do(&req)
		if err != nil {
			return nil, err
		}
		sets = append(sets, resp.ListSets.Set...)
		if !resp.HasResumptionToken() {
			break
		}
		token = resp.GetResumptionToken()
	}
	return sets, nil
}

func (r Repository) CompleteListSize() (int, error) {
	client := CreateClient(30*time.Second, 3)
	req := Request{BaseURL: r.BaseURL, Verb: "ListIdentifiers", MetadataPrefix: "oai_dc"}
	resp, err := client.Do(&req)
	if err != nil {
		return -1, err
	}
	size, err := strconv.Atoi(resp.ListIdentifiers.ResumptionToken.CompleteListSize)
	if err != nil {
		return -1, err
	}
	return size, nil
}

// FindRepositoriesByString returns a list of already harvested base URLs given a
// fragment of the base URL.
func FindRepositoriesByString(s string) (urls []string, err error) {
	files, err := ioutil.ReadDir(BaseDir)
	if err != nil {
		return urls, err
	}
	for _, file := range files {
		b, err := base64.RawURLEncoding.DecodeString(file.Name())
		if err != nil {
			return urls, err
		}
		parts := strings.SplitN(string(b), "#", 3)
		if len(parts) < 3 {
			continue
		}
		baseURL := parts[2]
		if strings.Contains(baseURL, s) {
			urls = append(urls, baseURL)
		}
	}
	return urls, nil
}
