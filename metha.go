// Package metha is the public face of the library: aliases for the types an
// importer depends on, and the pieces that belong to the program rather than to
// any one layer.
//
// The work is done in three packages, and the direction of the arrows is the
// point:
//
//	oai      the protocol - requests, responses, a client. No disk, no clock.
//	store    the cache - segments, the index, coverage. No network.
//	harvest  the planner and the driver. Imports both.
//
// The aliases below cost nothing and keep "github.com/miku/metha" meaning what
// it meant, but new code is better off importing the package it actually wants:
// a tool that only parses responses has no reason to link the cache, and nothing
// that imports this package can avoid it.
package metha

import (
	"context"
	"time"

	"github.com/miku/metha/harvest"
	"github.com/miku/metha/oai"
)

// The protocol. See package oai.
type (
	About               = oai.About
	Client              = oai.Client
	Description         = oai.Description
	Doer                = oai.Doer
	GetRecord           = oai.GetRecord
	HTTPError           = oai.HTTPError
	Header              = oai.Header
	Identify            = oai.Identify
	ListIdentifiers     = oai.ListIdentifiers
	ListMetadataFormats = oai.ListMetadataFormats
	ListRecords         = oai.ListRecords
	ListSets            = oai.ListSets
	Metadata            = oai.Metadata
	MetadataFormat      = oai.MetadataFormat
	OAIError            = oai.OAIError
	RateLimitedReader   = oai.RateLimitedReader
	Record              = oai.Record
	Repository          = oai.Repository
	Request             = oai.Request
	RequestNode         = oai.RequestNode
	Response            = oai.Response
	ResumptionToken     = oai.ResumptionToken
	Set                 = oai.Set
	Values              = oai.Values
)

// Harvesting. See package harvest. Sink is gone: a harvest writes through
// store.Writer, which it can name now that the packages are the way round they
// should have been.
type (
	Config   = harvest.Config
	Coverage = harvest.Coverage
	Harvest  = harvest.Harvest
	Interval = harvest.Interval
	Window   = harvest.Window
)

var (
	StdClient     = oai.StdClient
	DefaultClient = oai.DefaultClient

	// ErrAlreadySynced signals that a harvest found nothing left to fetch.
	ErrAlreadySynced = harvest.ErrAlreadySynced
	// ErrInvalidEarliestDate marks an endpoint whose granularity cannot be read.
	ErrInvalidEarliestDate = oai.ErrInvalidEarliestDate
)

const (
	// Day has 24 hours.
	Day = harvest.Day
	// SettleLag is how far back from the clock a harvest stops trusting an
	// endpoint that stamps records to the second.
	SettleLag = harvest.SettleLag
)

// Do runs one request with the default client.
func Do(r *Request) (*Response, error) { return oai.Do(r) }

// DoContext is Do, cancellable.
func DoContext(ctx context.Context, r *Request) (*Response, error) { return oai.DoContext(ctx, r) }

// CreateDoer returns an http client with a timeout and a retry policy.
func CreateDoer(timeout time.Duration, retries int) Doer { return oai.CreateDoer(timeout, retries) }

func CreateClient(timeout time.Duration, retries int) *Client {
	return oai.CreateClient(timeout, retries)
}

func CreateClientWithRateLimit(timeout time.Duration, retries int, bytesPerSec float64) *Client {
	return oai.CreateClientWithRateLimit(timeout, retries, bytesPerSec)
}

// NewHarvest creates a new harvest, using a network connection for an initial
// Identify request.
func NewHarvest(ctx context.Context, baseURL string) (*Harvest, error) {
	return harvest.NewHarvest(ctx, baseURL)
}

// PrependSchema prepends http, if it is missing.
func PrependSchema(s string) string { return oai.PrependSchema(s) }

// init hands the release version to the protocol package, which builds the
// User-Agent out of it. The version is injected here by the release build
// (-X github.com/miku/metha.Version=...), and oai has no business knowing which
// binary it was linked into.
func init() { oai.DefaultUserAgent = "metha/" + Version }
