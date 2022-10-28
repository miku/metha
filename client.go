package metha

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sethgrid/pester"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/html/charset"
)

const (
	// DefaultTimeout on requests.
	DefaultTimeout = 10 * time.Minute
	// DefaultMaxRetries is the default number of retries on a single request.
	DefaultMaxRetries = 8
)

var (
	// StdClient is the standard lib http client.
	StdClient = &Client{Doer: http.DefaultClient}
	// DefaultClient is the more resilient client, that will retry and timeout.
	DefaultClient = &Client{Doer: CreateDoer(DefaultTimeout, DefaultMaxRetries)}
	// DefaultUserAgent to identify crawler, some endpoints do not like the Go
	// default (https://golang.org/src/net/http/request.go#L462), e.g.
	// https://calhoun.nps.edu/oai/request.
	DefaultUserAgent = fmt.Sprintf("metha/%s", Version)
	// ControlCharReplacer helps to deal with broken XML: http://eprints.vu.edu.au/perl/oai2. Add more
	// weird things to be cleaned before XML parsing here. Another faulty:
	// http://digitalcommons.gardner-webb.edu/do/oai/?from=2016-02-29&metadataPr
	// efix=oai_dc&until=2016-03-31&verb=ListRecords. Replace control chars
	// outside XML char range.
	ControlCharReplacer = strings.NewReplacer(
		"\u0000", "", "\u0001", "", "\u0002", "", "\u0003", "", "\u0004", "",
		"\u0005", "", "\u0006", "", "\u0007", "", "\u0008", "", "\u0009", "",
		"\u000B", "", "\u000C", "", "\u000E", "", "\u000F", "", "\u0010", "",
		"\u0011", "", "\u0012", "", "\u0013", "", "\u0014", "", "\u0015", "",
		"\u0016", "", "\u0017", "", "\u0018", "", "\u0019", "", "\u001A", "",
		"\u001B", "", "\u001C", "", "\u001D", "", "\u001E", "", "\u001F", "",
		"\uFFFD", "", "\uFFFE", "",
	)
)

// HTTPError saves details of an HTTP error.
type HTTPError struct {
	URL          *url.URL
	StatusCode   int
	RequestError error
}

// Error prints the error message.
func (e HTTPError) Error() string {
	return fmt.Sprintf("failed with %s on %s: %v", http.StatusText(e.StatusCode), e.URL, e.RequestError)
}

// CreateDoer will return http request clients with specific timeout and retry
// properties.
func CreateDoer(timeout time.Duration, retries int) Doer {
	tr := http.DefaultTransport
	tr.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	client := http.DefaultClient
	client.Transport = tr
	if timeout == 0 && retries == 0 {
		return client
	}
	c := pester.New()
	c.EmbedHTTPClient(client)
	c.Timeout = timeout
	c.MaxRetries = retries
	c.Backoff = pester.ExponentialBackoff
	c.Transport = tr
	return c
}

// CreateClient creates a client with timeout and retry properties.
func CreateClient(timeout time.Duration, retries int) *Client {
	return &Client{Doer: CreateDoer(timeout, retries)}
}

// Doer is a minimal HTTP interface.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client can execute requests.
type Client struct {
	Doer Doer
}

// Do is a shortcut for DefaultClient.Do.
func Do(r *Request) (*Response, error) {
	return DefaultClient.Do(r)
}

// maybeCompressed detects compressed content and decompresses it on the fly.
func maybeCompressed(r io.Reader) (io.ReadCloser, error) {
	buf, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if gr, err := gzip.NewReader(bytes.NewReader(buf)); err == nil {
		log.Println("decompress-on-the-fly")
		return gr, nil
	}
	return ioutil.NopCloser(bytes.NewReader(buf)), nil
}

// Do executes a single OAIRequest. ResumptionToken handling must happen in the
// caller. Only Identify and GetRecord requests will return a complete response.
func (c *Client) Do(r *Request) (*Response, error) {
	link, err := r.URL()
	if err != nil {
		return nil, err
	}
	log.Println(link)

	req, err := http.NewRequest("GET", link.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", DefaultUserAgent) // Maybe https://codereview.appspot.com/7532043.
	for name, values := range r.ExtraHeaders {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	resp, err := c.Doer.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, HTTPError{URL: link, RequestError: err, StatusCode: resp.StatusCode}
	}
	defer resp.Body.Close()

	var reader = resp.Body

	// Detect compressed response.
	reader, err = maybeCompressed(reader)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	if r.CleanBeforeDecode {
		// Remove some chars, that the XML decoder will complain about.
		b, err := ioutil.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		reader = ioutil.NopCloser(strings.NewReader(ControlCharReplacer.Replace(string(b))))
	}

	// Drain response XML, iterate over various XML encoding declarations.
	// Limit the amount we can read.
	respBody, err := ioutil.ReadAll(io.LimitReader(reader, 2<<24))
	if err != nil {
		return nil, err
	}
	// refs #21812, hack around misleading XML declarations; we only cover
	// declared "UTF-8", but actually ... A rare issue nonetheless; add more
	// cases here if necessary; observed in the wild in 05/2022 at
	// http://digi.ub.uni-heidelberg.de/cgi-bin/digioai.cgi?from=2021-07-01T00:00:00Z&metadataPrefix=oai_dc&until=2021-07-31T23:59:59Z&verb=ListRecords.
	decls := [][]byte{
		[]byte(`<?xml version="1.0" encoding="UTF-8"?>`),
		[]byte(`<?xml version="1.0" encoding="ISO-8859-1"?>`),
		[]byte(`<?xml version="1.0" encoding="WINDOWS-1252"?>`),
		[]byte(`<?xml version="1.0" encoding="UTF-16"?>`),
		[]byte(`<?xml version="1.0" encoding="US-ASCII"?>`),
	}
	for i, decl := range decls {
		body := bytes.Replace(respBody, []byte(`<?xml version="1.0" encoding="UTF-8"?>`), decl, 1)
		dec := xml.NewDecoder(bytes.NewReader(body))
		dec.CharsetReader = charset.NewReaderLabel
		dec.Strict = false
		var response Response
		if err := dec.Decode(&response); err != nil {
			if !bytes.HasPrefix(body, []byte(`<?xml version="1.0"`)) {
				return nil, err
			}
			log.Printf("decode failed with: %v", string(decl))
			continue
		}
		if i > 0 {
			log.Printf("decode worked with adjusted declaration: %v", string(decl))
		}
		return &response, nil
	}
	return nil, fmt.Errorf("failed to parse response")
}
