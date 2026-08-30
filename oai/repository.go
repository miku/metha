package oai

import (
	"strconv"
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
