package metha

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/sethgrid/pester"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/html/charset"
	"golang.org/x/time/rate"
)

const (
	// DefaultTimeout on requests.
	DefaultTimeout = 10 * time.Minute
	// DefaultMaxRetries is the default number of retries on a single request.
	DefaultMaxRetries = 8
	// burstLimit for traffic shaping
	burstLimit = 1000 * 1000 * 1000
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

// RateLimitedReader wraps an io.Reader with rate limiting
type RateLimitedReader struct {
	r       io.Reader
	limiter *rate.Limiter
	ctx     context.Context
}

// NewRateLimitedReader creates a new rate limited reader
func NewRateLimitedReader(r io.Reader, ctx context.Context) *RateLimitedReader {
	return &RateLimitedReader{
		r:   r,
		ctx: ctx,
	}
}

// SetRateLimit sets rate limit (bytes/sec) to the reader.
func (s *RateLimitedReader) SetRateLimit(bytesPerSec float64) {
	s.limiter = rate.NewLimiter(rate.Limit(bytesPerSec), burstLimit)
	s.limiter.AllowN(time.Now(), burstLimit) // spend initial burst
}

// Read reads bytes into p with rate limiting.
func (s *RateLimitedReader) Read(p []byte) (int, error) {
	if s.limiter == nil {
		return s.r.Read(p)
	}
	n, err := s.r.Read(p)
	if err != nil {
		return n, err
	}
	if err := s.limiter.WaitN(s.ctx, n); err != nil {
		return n, err
	}
	return n, nil
}

// Close closes the underlying reader if it implements io.Closer
func (s *RateLimitedReader) Close() error {
	if c, ok := s.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// CreateDoer will return http request clients with specific timeout and retry
// properties.
func CreateDoer(timeout time.Duration, retries int) Doer {
	tr := http.DefaultTransport
	tr.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	client := http.DefaultClient
	client.Transport = tr
	client.Timeout = timeout
	if timeout == 0 && retries == 0 {
		return client
	}
	c := pester.New()
	c.RetryOnHTTP429 = true
	c.EmbedHTTPClient(client)
	c.Timeout = timeout // does this propagate to client
	c.MaxRetries = retries
	c.Backoff = pester.ExponentialBackoff
	c.Transport = tr
	return c
}

// CreateClient creates a client with timeout and retry properties.
func CreateClient(timeout time.Duration, retries int) *Client {
	return &Client{Doer: CreateDoer(timeout, retries)}
}

// CreateClientWithRateLimit creates a client with timeout, retry properties, and rate limiting.
func CreateClientWithRateLimit(timeout time.Duration, retries int, bytesPerSec float64) *Client {
	client := &Client{Doer: CreateDoer(timeout, retries)}
	client.SetRateLimit(bytesPerSec)
	return client
}

// Doer is a minimal HTTP interface.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client can execute requests.
type Client struct {
	Doer              Doer
	downloadRateLimit float64 // bytes per second, 0 means no limit
}

// SetRateLimit sets the download rate limit in bytes per second.
// Set to 0 to disable rate limiting.
func (c *Client) SetRateLimit(bytesPerSec float64) {
	c.downloadRateLimit = bytesPerSec
}

// GetRateLimit returns the current rate limit setting.
func (c *Client) GetRateLimit() float64 {
	return c.downloadRateLimit
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

	// Check for zstd magic number (0x28 0xB5 0x2F 0xFD)
	if len(buf) >= 4 && buf[0] == 0x28 && buf[1] == 0xB5 && buf[2] == 0x2F && buf[3] == 0xFD {
		zr, err := zstd.NewReader(bytes.NewReader(buf))
		if err == nil {
			log.Println("zstd-decompress-on-the-fly")
			return ioutil.NopCloser(zr), nil
		}
		// If zstd decompression fails, don't try gzip - it's definitely meant to be zstd
		return nil, fmt.Errorf("failed to decompress zstd data: %w", err)
	}

	// Check for gzip magic number (0x1F 0x8B)
	if len(buf) >= 2 && buf[0] == 0x1F && buf[1] == 0x8B {
		gr, err := gzip.NewReader(bytes.NewReader(buf))
		if err == nil {
			log.Println("gzip-decompress-on-the-fly")
			return gr, nil
		}
		// If gzip decompression fails, it's definitely meant to be gzip
		return nil, fmt.Errorf("failed to decompress gzip data: %w", err)
	}

	// No compression detected
	return ioutil.NopCloser(bytes.NewReader(buf)), nil
}

// wrapWithRateLimit wraps a reader with rate limiting if enabled
func (c *Client) wrapWithRateLimit(reader io.Reader, ctx context.Context) io.Reader {
	if c.downloadRateLimit <= 0 {
		return reader
	}

	rateLimitedReader := NewRateLimitedReader(reader, ctx)
	rateLimitedReader.SetRateLimit(c.downloadRateLimit)
	log.Printf("applying rate limit: %.2f bytes/sec", c.downloadRateLimit)
	return rateLimitedReader
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

	// Use request context for rate limiting
	ctx := req.Context()

	resp, err := c.Doer.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, HTTPError{URL: link, RequestError: err, StatusCode: resp.StatusCode}
	}
	defer resp.Body.Close()

	// Apply rate limiting to the response body if enabled
	var reader io.Reader = c.wrapWithRateLimit(resp.Body, ctx)

	// Detect compressed response.
	reader, err = maybeCompressed(reader)
	if err != nil {
		return nil, err
	}
	defer func() {
		if c, ok := reader.(io.Closer); ok {
			c.Close()
		}
	}()

	if r.CleanBeforeDecode {
		// Remove some chars, that the XML decoder will complain about.
		b, err := ioutil.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		reader = ioutil.NopCloser(strings.NewReader(ControlCharReplacer.Replace(string(b))))
	}
	// Drain response XML, iterate over various XML encoding declarations.
	// Limit the amount we can read, to 1GB, cf. https://github.com/miku/metha/issues/35
	respBody, err := ioutil.ReadAll(io.LimitReader(reader, 1<<30))
	if err != nil {
		return nil, err
	}
	// refs #21812, hack around misleading XML declarations; we only cover
	// declared "UTF-8", but actually ... A rare issue nonetheless; add more
	// cases here if necessary; observed in the wild in 05/2022 at
	// http://digi.ub.uni-heidelberg.de/cgi-bin/digioai.cgi?from=2021-07-01T00:00:00Z&metadataPrefix=oai_dc&until=2021-07-31T23:59:59Z&verb=ListRecords.
	//
	// TODO: https://github.com/miku/metha/issues/35
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
