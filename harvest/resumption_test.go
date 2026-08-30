// These tests cover the resumption-token end-of-harvest scenario reported in
// https://github.com/miku/metha/issues/43 — a server signaling the last page
// with an empty <resumptionToken/> element must stop the harvest rather than
// loop or restart from zero.
package harvest

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miku/metha/oai"
)

// minimalOAIListRecords wraps a body fragment into a valid OAI-PMH
// ListRecords response. The resumptionToken fragment is appended as-is, so
// callers can craft both empty (<resumptionToken/>) and non-empty variants.
func minimalOAIListRecords(resumptionTokenFragment string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/">
  <responseDate>2020-01-01T00:00:00Z</responseDate>
  <request verb="ListRecords" metadataPrefix="oai_dc">http://example.com/oai</request>
  <ListRecords>
    <record>
      <header>
        <identifier>oai:example.com:1</identifier>
        <datestamp>2020-01-01</datestamp>
      </header>
      <metadata><dc:title>record</dc:title></metadata>
    </record>
    %s
  </ListRecords>
</OAI-PMH>`, resumptionTokenFragment)
}

// parseXML is a small helper decoding raw OAI-PMH XML into a Response.
func parseXML(t *testing.T, s string) *oai.Response {
	t.Helper()
	var resp oai.Response
	dec := xml.NewDecoder(strings.NewReader(s))
	dec.Strict = false
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	return &resp
}

// TestGetResumptionTokenEmpty reproduces the scenario from the bug report: an
// OAI-PMH server signals the last page with an empty resumptionToken element.
// In all of these variants the harvest must consider the list complete, i.e.
// GetResumptionToken must return an empty string.
//
// Refs: https://www.openarchives.org/OAI/openarchivesprotocol.html#FlowControl
//
//	"the response containing the incomplete list that completes the list must
//	 include an empty resumptionToken element"
func TestGetResumptionTokenEmpty(t *testing.T) {
	cases := []struct {
		name      string
		fragment  string
		expectTok string
	}{
		{
			name:      "self-closing empty element",
			fragment:  "<resumptionToken/>",
			expectTok: "",
		},
		{
			name:      "empty open close element",
			fragment:  "<resumptionToken></resumptionToken>",
			expectTok: "",
		},
		{
			name:      "element with only whitespace chardata",
			fragment:  "<resumptionToken>   </resumptionToken>",
			expectTok: "   ", // raw, non-empty; documented current behavior
		},
		{
			name:      "non-empty token",
			fragment:  "<resumptionToken>abc123</resumptionToken>",
			expectTok: "abc123",
		},
		{
			name:      "no token element at all",
			fragment:  "",
			expectTok: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := parseXML(t, minimalOAIListRecords(tc.fragment))
			if got := resp.GetResumptionToken(); got != tc.expectTok {
				t.Errorf("GetResumptionToken() = %q, want %q", got, tc.expectTok)
			}
		})
	}
}

// sequentialDoer is a stateful Doer that replays a fixed sequence of raw XML
// bodies, one per call. It records the number of Do calls so tests can assert
// that the harvest did not over-fetch (e.g. loop forever after an empty token).
// After the sequence is exhausted it returns an HTTP error, so a misbehaving
// harvest fails loudly instead of looping indefinitely.
type sequentialDoer struct {
	mu     sync.Mutex
	bodies []string
	index  int
	calls  int
}

func (d *sequentialDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.index >= len(d.bodies) {
		return nil, fmt.Errorf("sequentialDoer: exhausted after %d response(s); harvest over-fetched", len(d.bodies))
	}
	body := d.bodies[d.index]
	d.index++
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// TestRunIntervalStopsOnEmptyResumptionToken runs a harvest against a mock
// server that, on the last page, returns an empty <resumptionToken/> element
// (exactly the case described in the bug report). The harvest must terminate
// after exactly two requests and must not loop or "start from zero again".
func TestRunIntervalStopsOnEmptyResumptionToken(t *testing.T) {
	doer := &sequentialDoer{
		bodies: []string{
			// Page 1: records plus a (non-empty) token for the next page.
			minimalOAIListRecords("<resumptionToken>token-page-2</resumptionToken>"),
			// Page 2 (last page): an empty resumptionToken element signals
			// completion per the OAI-PMH spec.
			minimalOAIListRecords("<resumptionToken/>"),
		},
	}
	base := t.TempDir()
	w := writerIn(t, base)
	h := &Harvest{
		Config: &Config{
			BaseURL:           testIdentity.BaseURL,
			Format:            testIdentity.Format,
			MaxRequests:       100, // high enough that it never triggers; the token must stop us
			MaxRetries:        0,
			RetryDelay:        time.Millisecond,
			RetryBackoff:      1.0,
			MaxEmptyResponses: 10, // same default as metha-sync
			// Run a single interval directly so the multiple-monthly-interval
			// splitting does not mask an over-fetch.
			DisableSelectiveHarvesting: true,
		},
		Client:  &oai.Client{Doer: doer},
		Writer:  w,
		Started: time.Now(),
		Identify: &oai.Identify{
			Granularity:       "YYYY-MM-DD",
			EarliestDatestamp: "2020-01-01",
		},
	}
	if err := h.runWindow(Window{}); err != nil {
		t.Fatalf("runWindow: %v", err)
	}

	if doer.calls != 2 {
		t.Fatalf("expected exactly 2 requests (page1 + last page), got %d", doer.calls)
	}

	// Both responses should have been committed, one per page, so the shard
	// holds the record each of them carried.
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := len(committed(t, base)); got != 2 {
		t.Fatalf("expected 2 harvested records, got %d", got)
	}
}

// TestRunIntervalStopsOnMissingResumptionToken is the companion case: a server
// that simply omits the resumptionToken element on the last page. The harvest
// must likewise stop after the second request.
func TestRunIntervalStopsOnMissingResumptionToken(t *testing.T) {
	doer := &sequentialDoer{
		bodies: []string{
			minimalOAIListRecords("<resumptionToken>token-page-2</resumptionToken>"),
			minimalOAIListRecords(""), // no token element at all
		},
	}
	base := t.TempDir()
	w := writerIn(t, base)
	h := &Harvest{
		Config: &Config{
			BaseURL:                    testIdentity.BaseURL,
			Format:                     testIdentity.Format,
			MaxRequests:                100,
			MaxRetries:                 0,
			RetryDelay:                 time.Millisecond,
			RetryBackoff:               1.0,
			MaxEmptyResponses:          10, // same default as metha-sync
			DisableSelectiveHarvesting: true,
		},
		Client:  &oai.Client{Doer: doer},
		Writer:  w,
		Started: time.Now(),
		Identify: &oai.Identify{
			Granularity:       "YYYY-MM-DD",
			EarliestDatestamp: "2020-01-01",
		},
	}
	if err := h.runWindow(Window{}); err != nil {
		t.Fatalf("runWindow: %v", err)
	}

	if doer.calls != 2 {
		t.Fatalf("expected exactly 2 requests, got %d", doer.calls)
	}
}

// TestRunIntervalStopsOnCursorEqualsCompleteListSize guards the separate
// heuristic where cursor == completeListSize is treated as end-of-harvest
// (refs #14865, e.g. DOAJ). The token is non-empty here, yet the harvest must
// stop after the single response.
func TestRunIntervalStopsOnCursorEqualsCompleteListSize(t *testing.T) {
	doer := &sequentialDoer{
		bodies: []string{
			minimalOAIListRecords(`<resumptionToken completeListSize="1" cursor="1">some-token</resumptionToken>`),
		},
	}
	base := t.TempDir()
	w := writerIn(t, base)
	h := &Harvest{
		Config: &Config{
			BaseURL:                    testIdentity.BaseURL,
			Format:                     testIdentity.Format,
			MaxRequests:                100,
			MaxRetries:                 0,
			RetryDelay:                 time.Millisecond,
			RetryBackoff:               1.0,
			MaxEmptyResponses:          10, // same default as metha-sync
			DisableSelectiveHarvesting: true,
		},
		Client:  &oai.Client{Doer: doer},
		Writer:  w,
		Started: time.Now(),
		Identify: &oai.Identify{
			Granularity:       "YYYY-MM-DD",
			EarliestDatestamp: "2020-01-01",
		},
	}
	if err := h.runWindow(Window{}); err != nil {
		t.Fatalf("runWindow: %v", err)
	}

	if doer.calls != 1 {
		t.Fatalf("expected exactly 1 request, got %d", doer.calls)
	}
}
