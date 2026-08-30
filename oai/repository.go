package oai

import (
	"errors"
	"fmt"
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

// ErrNoListSize marks an endpoint that did not say how many records it has and
// did not hand back the whole list either. completeListSize is an optional
// attribute of an optional element, so this is a legitimate answer rather than a
// failure, and callers are expected to carry on without the number.
var ErrNoListSize = errors.New("endpoint did not report a list size")

// CompleteListSize asks how many records an endpoint holds.
//
// One ListIdentifiers request, and three things it can come back as. The list
// may arrive whole, with no resumption token at all - which is the common case
// for a small repository, and then counting the headers is not an estimate but
// the exact answer. It may arrive with a token carrying completeListSize, which
// is the endpoint's own claim and the only case the first version of this
// handled. Or it may arrive with a token and no completeListSize, because the
// attribute is optional: nothing is known then, and saying so is the answer.
//
// The old form ran Atoi over the attribute unconditionally, so an endpoint whose
// records fit in one response - no token, no attribute, nothing wrong - failed
// with "strconv.Atoi: parsing \"\": invalid syntax".
func (r Repository) CompleteListSize() (int, error) {
	client := CreateClient(30*time.Second, 3)
	req := Request{BaseURL: r.BaseURL, Verb: "ListIdentifiers", MetadataPrefix: "oai_dc"}
	resp, err := client.Do(&req)
	if err != nil {
		return -1, err
	}
	// An endpoint that refused the question has not answered it. Without this,
	// a noRecordsMatch or a rate-limit reply falls through to the count below
	// and reports a repository of zero records, which is a different claim
	// entirely and a plausible-looking one.
	if resp.Error.Code != "" {
		return -1, resp.Error
	}
	if size := resp.ListIdentifiers.ResumptionToken.CompleteListSize; size != "" {
		n, err := strconv.Atoi(strings.TrimSpace(size))
		if err != nil {
			return -1, fmt.Errorf("completeListSize %q: %w", size, err)
		}
		return n, nil
	}
	// No size given. Without a token the response is the whole list, so it can
	// be counted; with one, the rest of the list is behind a token this does not
	// follow, and guessing from the first page would be worse than not answering.
	if resp.ListIdentifiers.ResumptionToken.Text != "" {
		return -1, fmt.Errorf("%w: %s", ErrNoListSize, r.BaseURL)
	}
	return len(resp.ListIdentifiers.Headers), nil
}
