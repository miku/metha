package metha

import (
	"net/url"
	"testing"
)

func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestURL(t *testing.T) {
	var tests = []struct {
		req Request
		u   *url.URL
		err error
	}{
		{req: Request{}, u: nil, err: ErrMissingURL},
		{req: Request{Verb: "x"}, u: nil, err: ErrMissingURL},
		{req: Request{BaseURL: "x", Verb: "x"}, u: nil, err: ErrInvalidVerb},
		{req: Request{BaseURL: "http://example.com", Verb: "x"}, u: nil, err: ErrInvalidVerb},
		{req: Request{BaseURL: "http://example.com", Verb: "Identify"}, u: mustParseURL("http://example.com?verb=Identify"), err: nil},
		{req: Request{BaseURL: "http://example.com", Verb: "ListRecords"}, u: nil, err: ErrParameterMissing},
		{req: Request{BaseURL: "http://example.com", Verb: "ListRecords", Set: "x"}, u: nil, err: ErrParameterMissing},
		{req: Request{BaseURL: "http://example.com", Verb: "ListRecords", MetadataPrefix: "x"}, u: mustParseURL("http://example.com?metadataPrefix=x&verb=ListRecords"), err: nil},
		{req: Request{BaseURL: "http://example.com", Verb: "ListRecords", MetadataPrefix: "x", From: "20"}, u: mustParseURL("http://example.com?from=20&metadataPrefix=x&verb=ListRecords"), err: nil},
		{req: Request{BaseURL: "http://example.com", Verb: "ListRecords", MetadataPrefix: "x", From: "20", ResumptionToken: "1"}, u: mustParseURL("http://example.com?resumptionToken=1&verb=ListRecords"), err: nil},
	}

	for _, test := range tests {
		u, err := test.req.URL()
		if err != test.err {
			t.Errorf("req.URL(%+v), got %v, want %v", test.req, err, test.err)
		}
		if err == nil {
			if u.String() != test.u.String() {
				t.Errorf("req.URL(%+v), got %v, want %v", test.req, u, test.u)
			}
		}
	}
}
