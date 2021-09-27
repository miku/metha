package metha

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
)

var (
	ErrInvalidVerb      = errors.New("invalid OAI verb")
	ErrMissingVerb      = errors.New("missing verb")
	ErrCannotGenerateID = errors.New("cannot generate ID")
	ErrMissingURL       = errors.New("missing URL")
	ErrParameterMissing = errors.New("missing required parameter")
)

// A Request can express any OAI request. Not all combination of values will
// yield valid requests.
type Request struct {
	BaseURL                 string
	Verb                    string
	Identifier              string
	MetadataPrefix          string
	From                    string
	Until                   string
	Set                     string
	ResumptionToken         string
	CleanBeforeDecode       bool
	SuppressFormatParameter bool
	ExtraHeaders            http.Header
}

// Values enhances the builtin url.Values.
type Values struct {
	url.Values
}

// NewValues create a new Values container.
func NewValues() Values {
	return Values{url.Values{}}
}

// EncodeVerbatim is like Encode(), but does not escape the keys and values.
func (v Values) EncodeVerbatim() string {
	if v.Values == nil {
		return ""
	}
	var buf bytes.Buffer
	keys := make([]string, 0, len(v.Values))
	for k := range v.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vs := v.Values[k]
		prefix := k + "="
		for _, v := range vs {
			if buf.Len() > 0 {
				buf.WriteByte('&')
			}
			buf.WriteString(prefix)
			buf.WriteString(v)
		}
	}
	return buf.String()
}

// URL returns the URL for a given request. Invalid verbs and missing parameters
// are reported here.
func (r *Request) URL() (*url.URL, error) {
	if r.BaseURL == "" {
		return nil, ErrMissingURL
	}

	v := NewValues()
	v.Add("verb", r.Verb)

	// An exclusive argument with a value that is the flow control token
	// returned by a previous a request that issued an incomplete list.
	if r.ResumptionToken != "" {
		v.Add("resumptionToken", r.ResumptionToken)
		var encodedValues string
		matched, _ := regexp.MatchString(` |\+`, r.ResumptionToken)
		if matched {
			// http://opencontext.org/oai/request has spaces in tokens
			// ExLibris Rosetta has + characters in tokens so encode in
			// Encoding in these cases
			encodedValues = v.Encode()
		} else {
			// Some repos, e.g. http://dash.harvard.edu/oai/request seem to have
			// problems with encoded tokens.
			encodedValues = v.EncodeVerbatim()
		}
		return url.Parse(fmt.Sprintf("%s?%s", r.BaseURL, encodedValues))
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
		if !r.SuppressFormatParameter {
			if err := addRequired("metadataPrefix", r.MetadataPrefix); err != nil {
				return nil, err
			}
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
		if !r.SuppressFormatParameter {
			if err := addRequired("metadataPrefix", r.MetadataPrefix); err != nil {
				return nil, err
			}
		}
	default:
		return nil, ErrInvalidVerb
	}
	// TODO(miku): some endpoints do not like encoded urls, e.g. http://web2.bium.univ-paris5.fr/oai-img/oai2.php
	return url.Parse(fmt.Sprintf("%s?%s", r.BaseURL, v.EncodeVerbatim()))
}
