package oai

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRepository_Formats(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("verb") != "ListMetadataFormats" {
			t.Errorf("expected verb ListMetadataFormats, got %s", r.URL.Query().Get("verb"))
		}
		response := &Response{
			ListMetadataFormats: ListMetadataFormats{
				MetadataFormat: []MetadataFormat{
					{
						MetadataPrefix:    "oai_dc",
						Schema:            "http://www.openarchives.org/OAI/2.0/oai_dc.xsd",
						MetadataNamespace: "http://www.openarchives.org/OAI/2.0/oai_dc/",
					},
					{
						MetadataPrefix:    "marc21",
						Schema:            "http://www.loc.gov/standards/marcxml/schema/MARC21slim.xsd",
						MetadataNamespace: "http://www.loc.gov/MARC21/slim",
					},
				},
			},
		}
		xmlData, _ := xml.Marshal(response)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(xmlData)
	}))
	defer server.Close()

	originalDoer := DefaultClient.Doer
	DefaultClient.Doer = &mockDoer{serverURL: server.URL}
	defer func() {
		DefaultClient.Doer = originalDoer
	}()
	var (
		repo         = Repository{BaseURL: server.URL}
		formats, err = repo.Formats()
	)
	if err != nil {
		t.Fatalf("Formats() returned error: %v", err)
	}
	if len(formats) != 2 {
		t.Errorf("expected 2 formats, got %d", len(formats))
	}
	if formats[0].MetadataPrefix != "oai_dc" {
		t.Errorf("expected first format to have metadataPrefix 'oai_dc', got '%s'", formats[0].MetadataPrefix)
	}
	if formats[1].MetadataPrefix != "marc21" {
		t.Errorf("expected second format to have metadataPrefix 'marc21', got '%s'", formats[1].MetadataPrefix)
	}
}

func TestRepository_Sets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("verb") != "ListSets" {
			t.Errorf("expected verb ListSets, got %s", r.URL.Query().Get("verb"))
		}
		response := &Response{
			ListSets: ListSets{
				Set: []Set{
					{
						SetSpec: "set1",
						SetName: "First Set",
					},
					{
						SetSpec: "set2",
						SetName: "Second Set",
					},
				},
			},
		}

		xmlData, _ := xml.Marshal(response)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(xmlData)
	}))
	defer server.Close()

	originalDoer := DefaultClient.Doer
	DefaultClient.Doer = &mockDoer{serverURL: server.URL}
	defer func() {
		DefaultClient.Doer = originalDoer
	}()
	var (
		repo      = Repository{BaseURL: server.URL}
		sets, err = repo.Sets()
	)
	if err != nil {
		t.Fatalf("Sets() returned error: %v", err)
	}
	if len(sets) != 2 {
		t.Errorf("expected 2 sets, got %d", len(sets))
	}
	if sets[0].SetSpec != "set1" {
		t.Errorf("expected first set to have setSpec 'set1', got '%s'", sets[0].SetSpec)
	}
	if sets[1].SetName != "Second Set" {
		t.Errorf("expected second set to have setName 'Second Set', got '%s'", sets[1].SetName)
	}
}

func TestRepository_CompleteListSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("verb") != "ListIdentifiers" {
			t.Errorf("expected verb ListIdentifiers, got %s", r.URL.Query().Get("verb"))
		}
		if r.URL.Query().Get("metadataPrefix") != "oai_dc" {
			t.Errorf("expected metadataPrefix 'oai_dc', got %s", r.URL.Query().Get("metadataPrefix"))
		}
		response := &Response{
			ListIdentifiers: ListIdentifiers{
				ResumptionToken: ResumptionToken{
					Text:             "token123",
					CompleteListSize: "100",
				},
			},
		}

		xmlData, _ := xml.Marshal(response)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(xmlData)
	}))
	defer server.Close()
	originalDoer := DefaultClient.Doer
	DefaultClient.Doer = &mockDoer{serverURL: server.URL}
	defer func() {
		DefaultClient.Doer = originalDoer
	}()
	var (
		repo      = Repository{BaseURL: server.URL}
		size, err = repo.CompleteListSize()
	)
	if err != nil {
		t.Fatalf("CompleteListSize() returned error: %v", err)
	}
	if size != 100 {
		t.Errorf("expected size 100, got %d", size)
	}
}

// mockDoer implements the Doer interface to mock HTTP requests
type mockDoer struct {
	serverURL string
}

func (m *mockDoer) Do(req *http.Request) (*http.Response, error) {
	newReq, err := http.NewRequest(req.Method, m.serverURL+req.URL.RequestURI(), nil)
	if err != nil {
		return nil, err
	}
	for name, values := range req.Header {
		for _, value := range values {
			newReq.Header.Add(name, value)
		}
	}
	return http.DefaultClient.Do(newReq)
}

// TestRepository_SetsStopsOnACompletedList: a set list whose last page says so
// with a cursor that has reached completeListSize ends the walk.
//
// The loop used to ask HasResumptionToken, which is true whenever the element is
// there at all, and then send GetResumptionToken - which drops exactly that
// token, because a cursor at the end of the list is how an endpoint says there
// is no more. The empty token repeated the first request, and the first request
// answered the same way: an endless loop appending the same two sets to the
// slice until the process ran out of memory.
func TestRepository_SetsStopsOnACompletedList(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests > 4 {
			t.Errorf("Sets() is not terminating: %d requests", requests)
			http.Error(w, "too many requests", http.StatusInternalServerError)
			return
		}
		response := &Response{
			ListSets: ListSets{
				Set: []Set{{SetSpec: "set1", SetName: "First Set"}},
				ResumptionToken: ResumptionToken{
					Text:             "more-sets",
					Cursor:           "2",
					CompleteListSize: "2",
				},
			},
		}
		xmlData, _ := xml.Marshal(response)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(xmlData)
	}))
	defer server.Close()

	originalDoer := DefaultClient.Doer
	DefaultClient.Doer = &mockDoer{serverURL: server.URL}
	defer func() { DefaultClient.Doer = originalDoer }()

	sets, err := Repository{BaseURL: server.URL}.Sets()
	if err != nil {
		t.Fatalf("Sets() returned error: %v", err)
	}
	if len(sets) != 1 {
		t.Errorf("got %d set(s), want 1", len(sets))
	}
	if requests != 1 {
		t.Errorf("made %d request(s), want 1", requests)
	}
}
