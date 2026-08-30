package oai

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCompleteListSize covers what a ListIdentifiers response can say about how
// big a repository is, which is more than one thing.
//
// completeListSize is an optional attribute of an optional element, and the
// first version of this read it unconditionally: an endpoint whose records fit
// in a single response - no token, no attribute, nothing wrong - came back as
//
//	metha: strconv.Atoi: parsing "": invalid syntax
//
// which is the whole of what the user saw for a perfectly ordinary journal of 25
// articles.
func TestCompleteListSize(t *testing.T) {
	const header = `<header><identifier>oai:example.org:%d</identifier><datestamp>2023-01-01</datestamp></header>`

	// identifiers renders n headers, optionally followed by a resumption token.
	identifiers := func(n int, token string) string {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListIdentifiers>`)
		for i := range n {
			fmt.Fprintf(&b, header, i)
		}
		b.WriteString(token)
		b.WriteString(`</ListIdentifiers></OAI-PMH>`)
		return b.String()
	}

	for _, tt := range []struct {
		name string
		body string
		want int
		err  error
	}{
		{
			// The reported case. 25 articles, one response, no token at all -
			// so the count is not an estimate, it is the answer.
			name: "the whole list in one response",
			body: identifiers(25, ""),
			want: 25,
		},
		{
			name: "a token that says how many there are",
			body: identifiers(100, `<resumptionToken completeListSize="4242" cursor="0">tok</resumptionToken>`),
			want: 4242,
		},
		{
			// Legal, and what arXiv does. Counting the first page would be
			// worse than not answering: it looks like a size and is not one.
			name: "a token that does not say",
			body: identifiers(100, `<resumptionToken cursor="0">tok</resumptionToken>`),
			err:  ErrNoListSize,
		},
		{
			name: "whitespace around the number",
			body: identifiers(10, `<resumptionToken completeListSize=" 512 " cursor="0">tok</resumptionToken>`),
			want: 512,
		},
		{
			// An empty repository really does hold nothing, and says so the
			// same way a small one does.
			name: "an empty repository",
			body: identifiers(0, ""),
			want: 0,
		},
		{
			// The hole the fix had to avoid: no headers and no token looks
			// exactly like an empty repository, and reporting zero would be a
			// confident wrong answer rather than a missing one.
			name: "an endpoint that refused the question",
			body: `<?xml version="1.0"?><OAI-PMH><error code="noRecordsMatch">nothing here</error></OAI-PMH>`,
			err:  errNoRecordsMatch,
		},
		{
			name: "a size that is not a number",
			body: identifiers(10, `<resumptionToken completeListSize="lots" cursor="0">tok</resumptionToken>`),
			err:  errNotANumber,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("verb"); got != "ListIdentifiers" {
					t.Errorf("verb %q, want ListIdentifiers", got)
				}
				w.Header().Set("Content-Type", "application/xml")
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			size, err := Repository{BaseURL: server.URL}.CompleteListSize()
			switch {
			case tt.err == nil && err != nil:
				t.Fatalf("CompleteListSize: %v, want %d", err, tt.want)
			case tt.err == nil:
				if size != tt.want {
					t.Errorf("CompleteListSize = %d, want %d", size, tt.want)
				}
			case err == nil:
				t.Fatalf("CompleteListSize = %d, want an error", size)
			default:
				if !matches(err, tt.err) {
					t.Errorf("CompleteListSize: %v, want %v", err, tt.err)
				}
				if size != -1 {
					t.Errorf("CompleteListSize returned %d alongside an error, want -1", size)
				}
			}
		})
	}
}

// errNoRecordsMatch and errNotANumber stand for outcomes that are recognised by
// shape rather than by identity: an OAI error carries the endpoint's own code,
// and a bad number carries whatever strconv said.
var (
	errNoRecordsMatch = errors.New("noRecordsMatch")
	errNotANumber     = errors.New("invalid syntax")
)

func matches(got, want error) bool {
	return errors.Is(got, want) || strings.Contains(got.Error(), want.Error())
}
