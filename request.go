package perimorph

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	ErrInvalidVerb      = errors.New("invalid OAI verb")
	ErrMissingVerb      = errors.New("missing verb")
	ErrCannotGenerateID = errors.New("cannot generate ID")
	ErrMissingURL       = errors.New("missing URL")
	ErrParameterMissing = errors.New("missing required parameter")
)

// A Request can express any request, that can be sent to an OAI server. Not all
// combination of values will yield valid requests.
type Request struct {
	BaseURL           string
	Verb              string
	Identifier        string
	MetadataPrefix    string
	From              string
	Until             string
	Set               string
	ResumptionToken   string
	CleanBeforeDecode bool
}

// URL returns the URL for a given request. Invalid verbs and missing parameters
// are reported here.
func (r *Request) URL() (*url.URL, error) {
	if r.BaseURL == "" {
		return nil, ErrMissingURL
	}

	v := url.Values{}
	v.Add("verb", r.Verb)

	// An exclusive argument with a value that is the flow control token
	// returned by a previous a request that issued an incomplete list.
	if r.ResumptionToken != "" {
		v.Add("resumptionToken", r.ResumptionToken)
		// http://opencontext.org/oai/request has spaces in tokens so encode in
		// this case.
		if strings.Contains(r.ResumptionToken, " ") {
			return url.Parse(fmt.Sprintf("%s?%s", r.BaseURL, v.Encode()))
		}
		// Some repos, e.g. http://dash.harvard.edu/oai/request seem to have
		// problems with encoded tokens.
		return url.Parse(fmt.Sprintf("%s?verb=%s&resumptionToken=%s",
			r.BaseURL, v.Get("verb"), v.Get("resumptionToken")))
	}

	// Only add parameter, if it is not the zero value.
	addOptional := func(key, value string) {
		if value != "" {
			v.Add(key, value)
		}
	}

	// If required parameter is missing, complain.
	addRequired := func(key, value string) error {
		if value == "" {
			return ErrParameterMissing
		}
		v.Add(key, value)
		return nil
	}

	switch r.Verb {
	case "ListMetadataFormats", "ListSets":
	case "ListIdentifiers", "ListRecords":
		if err := addRequired("metadataPrefix", r.MetadataPrefix); err != nil {
			return nil, err
		}
		addOptional("from", r.From)
		addOptional("until", r.Until)
		addOptional("set", r.Set)
	case "Identify":
		addOptional("identifier", r.Identifier)
	case "GetRecord":
		if err := addRequired("identifier", r.Identifier); err != nil {
			return nil, err
		}
		if err := addRequired("metadataPrefix", r.MetadataPrefix); err != nil {
			return nil, err
		}
	default:
		return nil, ErrInvalidVerb
	}
	// TODO(miku): some endpoints do not like encoded urls, e.g. http://web2.bium.univ-paris5.fr/oai-img/oai2.php
	return url.Parse(fmt.Sprintf("%s?%s", r.BaseURL, v.Encode()))
}
