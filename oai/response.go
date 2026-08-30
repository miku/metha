package oai

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nytlabs/mxj"
	log "github.com/sirupsen/logrus"
)

// ResupmtionToken with optional extra information.
type ResumptionToken struct {
	Text             string `xml:",chardata"` // eyJhIjogWyIyMDE5LTAyLTIxV...
	CompleteListSize string `xml:"completeListSize,attr"`
	Cursor           string `xml:"cursor,attr"`
	ExpirationDate   string `xml:"expirationDate,attr"`
}

// Response is the envelope. It can hold any OAI response kind.
type Response struct {
	ResponseDate string      `xml:"responseDate,omitempty" json:"responseDate,omitempty"`
	Request      RequestNode `xml:"request,omitempty" json:"request,omitempty"`
	Error        OAIError    `xml:"error,omitempty" json:"error,omitempty"`

	GetRecord           GetRecord           `xml:"GetRecord,omitempty" json:"GetRecord,omitempty"`
	Identify            Identify            `xml:"Identify,omitempty" json:"Identify,omitempty"`
	ListIdentifiers     ListIdentifiers     `xml:"ListIdentifiers,omitempty" json:"ListIdentifiers,omitempty"`
	ListMetadataFormats ListMetadataFormats `xml:"ListMetadataFormats,omitempty" json:"ListMetadataFormats,omitempty"`
	ListRecords         ListRecords         `xml:"ListRecords,omitempty" json:"ListRecords,omitempty"`
	ListSets            ListSets            `xml:"ListSets,omitempty" json:"ListSets,omitempty"`

	// Raw is the document this response was decoded from, and it is what the
	// cache stores. A struct is what the decoder could make of a response;
	// the document is what the endpoint actually said, and the difference is
	// everything the decoder had no field for - which is gone for good once
	// only the struct is kept. metha is a cache of responses, so it keeps the
	// responses.
	//
	// These are the bytes the decode succeeded on, not the bytes off the
	// socket: an on-the-fly gzip or zstd body is decompressed first, control
	// characters are replaced when the caller asked for that, and a misdeclared
	// encoding has been corrected. Storing anything earlier would put documents
	// in the cache that the cache's own reader cannot parse.
	//
	// Excluded from both encodings. It is what a response was, not a field of
	// one, and marshalling a Response back out must not carry it.
	Raw []byte `xml:"-" json:"-"`
}

// ErrInvalidEarliestDate marks an endpoint whose advertised granularity is
// neither of the two forms the protocol allows, so nothing it says about dates
// can be read.
var ErrInvalidEarliestDate = errors.New("invalid earliest date")

// Identify reports information about a repository.
type Identify struct {
	RepositoryName    string        `xml:"repositoryName,omitempty" json:"repositoryName,omitempty"`
	BaseURL           string        `xml:"baseURL,omitempty" json:"baseURL,omitempty"`
	ProtocolVersion   string        `xml:"protocolVersion,omitempty" json:"protocolVersion,omitempty"`
	AdminEmail        []string      `xml:"adminEmail,omitempty" json:"adminEmail,omitempty"`
	EarliestDatestamp string        `xml:"earliestDatestamp,omitempty" json:"earliestDatestamp,omitempty"`
	DeletedRecord     string        `xml:"deletedRecord,omitempty" json:"deletedRecord,omitempty"`
	Granularity       string        `xml:"granularity,omitempty" json:"granularity,omitempty"`
	Description       []Description `xml:"description,omitempty" json:"description,omitempty"`
}

// IsEmpty reports whether an Identify carries nothing at all, which is what a
// URL that is not an OAI-PMH endpoint answers with.
//
// The decoder is deliberately lenient - endpoints send a great deal that is not
// quite XML, and refusing it would lose the responses most worth keeping - so a
// home page, a 200-with-an-error-page, or any other document without an
// <Identify> element decodes without complaint into the zero value of this
// struct rather than failing. Nothing distinguishes that from a reply, except
// that a reply says something.
//
// Every field is checked rather than one, because endpoints omit things the
// protocol requires. An endpoint that answers with a repository name and
// nothing else is broken but real, and can still be harvested whole with
// -no-intervals; one that answers with no field at all was not asked a question
// it understood.
func (id *Identify) IsEmpty() bool {
	if id == nil {
		return true
	}
	return id.RepositoryName == "" &&
		id.BaseURL == "" &&
		id.ProtocolVersion == "" &&
		len(id.AdminEmail) == 0 &&
		id.EarliestDatestamp == "" &&
		id.DeletedRecord == "" &&
		id.Granularity == "" &&
		len(id.Description) == 0
}

// granularity is the endpoint's advertised granularity, folded to lower case.
// The spec gives the two forms in a fixed case, but enough endpoints get that
// wrong that reading them literally would drop the bounds from every request;
// EarliestDate has always compared them this way. An endpoint that was never
// asked answers the empty string, which is neither form.
func (id *Identify) granularity() string {
	if id == nil {
		return ""
	}
	return strings.ToLower(id.Granularity)
}

// SecondGranularity reports whether the endpoint stamps records to the second.
// An endpoint that says nothing intelligible about its granularity - or that was
// never asked - is treated as the coarser of the two, which is the assumption
// that cannot lose records.
func (id *Identify) SecondGranularity() bool {
	return id.granularity() == "yyyy-mm-ddthh:mm:ssz"
}

// DayGranularity reports whether the endpoint stamps records to the day.
func (id *Identify) DayGranularity() bool {
	return id.granularity() == "yyyy-mm-dd"
}

// EarliestDate is the endpoint's earliest datestamp, parsed as the granularity
// it advertises spells it. An endpoint that advertises neither form is refused:
// nothing else it says about dates can be believed either.
func (id *Identify) EarliestDate() (time.Time, error) {
	// Different granularities are possible: https://eudml.org/oai/OAIHandler?verb=Identify
	// First occurence of a non-standard granularity: https://t3.digizeitschriften.de/oai2/
	switch id.granularity() {
	case "yyyy-mm-dd":
		if len(id.EarliestDatestamp) <= 10 {
			return time.Parse("2006-01-02", id.EarliestDatestamp)
		}
		return time.Parse("2006-01-02", id.EarliestDatestamp[:10])
	case "yyyy-mm-ddthh:mm:ssz":
		// refs. #8825
		if len(id.EarliestDatestamp) >= 10 && len(id.EarliestDatestamp) < 20 {
			return time.Parse("2006-01-02", id.EarliestDatestamp[:10])
		}
		return time.Parse("2006-01-02T15:04:05Z", id.EarliestDatestamp)
	default:
		return time.Time{}, ErrInvalidEarliestDate
	}
}

// ListSets lists available sets.
type ListSets struct {
	Set             []Set           `xml:"set,omitempty"  json:"set,omitempty"`
	ResumptionToken ResumptionToken `xml:"resumptionToken,omitempty" json:"resumptionToken,omitempty"`
}

// A Set has a spec, name and description.
type Set struct {
	SetSpec        string      `xml:"setSpec,omitempty" json:"setSpec,omitempty"`
	SetName        string      `xml:"setName,omitempty" json:"setName,omitempty"`
	SetDescription Description `xml:"setDescription,omitempty" json:"setDescription,omitempty"`
}

// A Header is part of other requests.
type Header struct {
	Status     string   `xml:"status,attr" json:"status,omitempty"`
	Identifier string   `xml:"identifier,omitempty" json:"identifier,omitempty"`
	DateStamp  string   `xml:"datestamp,omitempty" json:"datestamp,omitempty"`
	SetSpec    []string `xml:"setSpec,omitempty" json:"setSpec,omitempty"`
}

// Metadata contains the actual metadata, conforming to varying schemas.
type Metadata struct {
	Body []byte `xml:",innerxml"`
}

// MarshalJSON marshals the metadata body.
func (md Metadata) MarshalJSON() ([]byte, error) {
	if len(md.Body) == 0 {
		return []byte("{}"), nil
	}
	// TODO: Is there a more uniform way to create JSON, e.g. one that has some
	// listify option, like xmltodict?
	m, err := mxj.NewMapXmlReader(bytes.NewReader(md.Body))
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// GoString is a formatter for Metadata content.
func (md Metadata) GoString() string { return string(md.Body) }

// About has addition record information.
type About struct {
	Body []byte `xml:",innerxml" json:"body,omitempty"`
}

// GoString is a formatter for About content.
func (ab About) GoString() string { return string(ab.Body) }

// Record represents a single record.
type Record struct {
	XMLName  xml.Name
	Header   Header   `xml:"header,omitempty" json:"header,omitempty"`
	Metadata Metadata `xml:"metadata,omitempty" json:"metadata,omitempty"`
	About    About    `xml:"about,omitempty" json:"about,omitempty"`
}

// Deleted reports whether the endpoint has marked this record as deleted. The
// status attribute is the only place that is said; the record still carries its
// identifier and datestamp, and usually an empty metadata element.
func (rec Record) Deleted() bool { return rec.Header.Status == "deleted" }

// ListIdentifiers lists headers only.
type ListIdentifiers struct {
	Headers         []Header        `xml:"header,omitempty" json:"header,omitempty"`
	ResumptionToken ResumptionToken `xml:"resumptionToken,omitempty" json:"resumptionToken,omitempty"`
}

// ListRecords lists records.
type ListRecords struct {
	Records         []Record        `xml:"record" json:"record"`
	ResumptionToken ResumptionToken `xml:"resumptionToken,omitempty" json:"resumptionToken,omitempty"`
}

// GetRecord returns a single record.
type GetRecord struct {
	Record Record `xml:"record,omitempty" json:"record,omitempty"`
}

// RequestNode carries the request information into the response.
type RequestNode struct {
	Verb           string `xml:"verb,attr" json:"verb,omitempty"`
	Set            string `xml:"set,attr" json:"set,omitempty"`
	MetadataPrefix string `xml:"metadataPrefix,attr" json:"metadataPrefix,omitempty"`
}

// OAIError is an OAI protocol error.
type OAIError struct {
	Code    string `xml:"code,attr" json:"code,omitempty"`
	Message string `xml:",chardata" json:"message,omitempty"`
}

// Error formats code and message.
func (e OAIError) Error() string {
	return fmt.Sprintf("oai: %s %s", e.Code, e.Message)
}

// MetadataFormat holds information about a format.
type MetadataFormat struct {
	MetadataPrefix    string `xml:"metadataPrefix,omitempty" json:"metadataPrefix,omitempty"`
	Schema            string `xml:"schema,omitempty" json:"schema,omitempty"`
	MetadataNamespace string `xml:"metadataNamespace,omitempty" json:"metadataNamespace,omitempty"`
}

// ListMetadataFormats lists supported metadata formats.
type ListMetadataFormats struct {
	MetadataFormat []MetadataFormat `xml:"metadataFormat,omitempty" json:"metadataFormat,omitempty"`
}

// Description holds information about a set.
type Description struct {
	Body []byte `xml:",innerxml"`
}

// GoString is a formatter for Description content.
func (desc Description) GoString() string { return string(desc.Body) }

// HasResumptionToken determines if the request has a ResumptionToken.
func (response *Response) HasResumptionToken() bool {
	return response.ListSets.ResumptionToken.Text != "" ||
		response.ListIdentifiers.ResumptionToken.Text != "" ||
		response.ListRecords.ResumptionToken.Text != ""
}

// CompleteListSize returns the value of completeListSize, if it exists.
func (response *Response) CompleteListSize() string {
	if response.ListSets.ResumptionToken.CompleteListSize != "" {
		return response.ListSets.ResumptionToken.CompleteListSize
	}
	if response.ListIdentifiers.ResumptionToken.CompleteListSize != "" {
		return response.ListIdentifiers.ResumptionToken.CompleteListSize
	}
	if response.ListRecords.ResumptionToken.CompleteListSize != "" {
		return response.ListRecords.ResumptionToken.CompleteListSize
	}
	return ""
}

// CompleteListSize returns the value of completeListSize, if it exists.
func (response *Response) Cursor() string {
	if response.ListSets.ResumptionToken.Cursor != "" {
		return response.ListSets.ResumptionToken.Cursor
	}
	if response.ListIdentifiers.ResumptionToken.Cursor != "" {
		return response.ListIdentifiers.ResumptionToken.Cursor
	}
	if response.ListRecords.ResumptionToken.Cursor != "" {
		return response.ListRecords.ResumptionToken.Cursor
	}
	return ""
}

// GetResumptionToken returns the resumption token or an empty string if it
// does not have a token. In addition, return an empty string, if cursor and
// complete list size are defined and are equal (doaj, refs #14865).
func (response *Response) GetResumptionToken() string {

	// If cursor and complete list size are non-empty and equal, we take it as
	// a signal to stop harvesting.
	if len(response.CompleteListSize()) > 0 && len(response.Cursor()) > 0 && response.CompleteListSize() == response.Cursor() {
		log.Printf("cursor and complete list size match (%d), ignoring any token", len(response.Cursor()))
		return ""
	}

	// First attempt to obtain a resumption token from a ListIdentifiers response
	resumptionToken := response.ListIdentifiers.ResumptionToken.Text

	// Then attempt to obtain a resumption token from a ListRecords response
	if resumptionToken == "" {
		resumptionToken = response.ListRecords.ResumptionToken.Text
	}
	// Then attempt to obtain a resumption token from a ListSets response
	if resumptionToken == "" {
		resumptionToken = response.ListSets.ResumptionToken.Text
	}
	return resumptionToken
}
