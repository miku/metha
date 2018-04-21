package metha

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/nytlabs/mxj"
)

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
}

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

// ListSets lists available sets. TODO(miku): resumptiontoken can have expiration date, etc.
type ListSets struct {
	Set             []Set  `xml:"set,omitempty"  json:"set,omitempty"`
	ResumptionToken string `xml:"resumptionToken,omitempty" json:"resumptionToken,omitempty"`
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
	m, err := mxj.NewMapXmlReader(bytes.NewReader(md.Body))
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// GoString is a formatter for Metadata content.
func (md Metadata) GoString() string { return fmt.Sprintf("%s", md.Body) }

// About has addition record information.
type About struct {
	Body []byte `xml:",innerxml" json:"body,omitempty"`
}

// GoString is a formatter for About content.
func (ab About) GoString() string { return fmt.Sprintf("%s", ab.Body) }

// Record represents a single record.
type Record struct {
	Header   Header   `xml:"header,omitempty" json:"header,omitempty"`
	Metadata Metadata `xml:"metadata,omitempty" json:"metadata,omitempty"`
	About    About    `xml:"about,omitempty" json:"about,omitempty"`
}

// ListIdentifiers lists headers only.
type ListIdentifiers struct {
	Headers         []Header `xml:"header,omitempty" json:"header,omitempty"`
	ResumptionToken string   `xml:"resumptionToken,omitempty" json:"resumptionToken,omitempty"`
}

// ListRecords lists records.
type ListRecords struct {
	Records         []Record `xml:"record" json:"record"`
	ResumptionToken string   `xml:"resumptionToken" json:"resumptionToken"`
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
func (desc Description) GoString() string { return fmt.Sprintf("%s", desc.Body) }

// HasResumptionToken determines if the request has a ResumptionToken.
func (response *Response) HasResumptionToken() bool {
	return response.ListSets.ResumptionToken != "" ||
		response.ListIdentifiers.ResumptionToken != "" ||
		response.ListRecords.ResumptionToken != ""
}

// GetResumptionToken returns the resumption token or an empty string
// if it does not have a token
func (response *Response) GetResumptionToken() string {

	// First attempt to obtain a resumption token from a ListIdentifiers response
	resumptionToken := response.ListIdentifiers.ResumptionToken

	// Then attempt to obtain a resumption token from a ListRecords response
	if resumptionToken == "" {
		resumptionToken = response.ListRecords.ResumptionToken
	}
	// Then attempt to obtain a resumption token from a ListSets response
	if resumptionToken == "" {
		resumptionToken = response.ListSets.ResumptionToken
	}
	return resumptionToken
}
