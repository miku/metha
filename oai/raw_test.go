package oai

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
)

// bodyDoer answers every request with one canned body.
type bodyDoer struct{ body []byte }

func (d *bodyDoer) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(d.body)),
		Header:     make(http.Header),
	}, nil
}

// TestResponseKeepsRaw: a Response carries the document it was decoded from,
// because that document is what the cache stores. Decoding into the struct and
// marshalling it back out was the old way, and it wrote back only the fields
// this package happens to model - every extension element and every attribute
// nothing here knows about was dropped on the way in.
func TestResponseKeepsRaw(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<OAI-PMH xmlns:custom="http://example.com/x"><ListRecords>` +
		`<record><header><identifier>a</identifier><datestamp>2023-01-01</datestamp>` +
		`<custom:provenance>kept</custom:provenance></header>` +
		`<metadata><dc><title>T</title></dc></metadata></record>` +
		`</ListRecords></OAI-PMH>`

	c := &Client{Doer: &bodyDoer{body: []byte(body)}}
	resp, err := c.Do(&Request{BaseURL: "http://example.com/oai", Verb: "ListRecords", MetadataPrefix: "oai_dc"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := string(resp.Raw); got != body {
		t.Errorf("Raw is not the document that arrived:\ngot  %s\nwant %s", got, body)
	}
	// The thing the round trip through the struct used to lose. It is not
	// reachable through any field of Response, which is the point: it is in the
	// bytes, so a later reader can still get at it.
	if !strings.Contains(string(resp.Raw), "custom:provenance") {
		t.Error("Raw dropped an element this package does not model")
	}
	// And the decode still happened, so nothing is traded for keeping it.
	if n := len(resp.ListRecords.Records); n != 1 {
		t.Errorf("decoded %d records, want 1", n)
	}
}

// TestRawIsNotMarshalled: Raw is what a response was, not a field of one. A
// Response written back out as XML or JSON must not carry a copy of its own
// source, which would double every document that passed through.
func TestRawIsNotMarshalled(t *testing.T) {
	body := `<OAI-PMH><ListRecords><record><header><identifier>a</identifier></header></record></ListRecords></OAI-PMH>`
	c := &Client{Doer: &bodyDoer{body: []byte(body)}}
	resp, err := c.Do(&Request{BaseURL: "http://example.com/oai", Verb: "ListRecords", MetadataPrefix: "oai_dc"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	for _, tt := range []struct {
		name string
		out  func() ([]byte, error)
	}{
		{"xml", func() ([]byte, error) { return xml.Marshal(resp) }},
		{"json", func() ([]byte, error) { return json.Marshal(resp) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, err := tt.out()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if bytes.Contains(b, []byte("Raw")) || bytes.Contains(b, []byte("OAI-PMH")) {
				t.Errorf("marshalled form carries the raw document: %s", b)
			}
		})
	}
}
