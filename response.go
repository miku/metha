package metha

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"

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
func (md Metadata) GoString() string { return fmt.Sprintf("%s", md.Body) }

// About has addition record information.
type About struct {
	Body []byte `xml:",innerxml" json:"body,omitempty"`
}

// GoString is a formatter for About content.
func (ab About) GoString() string { return fmt.Sprintf("%s", ab.Body) }

// Record represents a single record.
type Record struct {
	XMLName  xml.Name
	Header   Header   `xml:"header,omitempty" json:"header,omitempty"`
	Metadata Metadata `xml:"metadata,omitempty" json:"metadata,omitempty"`
	About    About    `xml:"about,omitempty" json:"about,omitempty"`
}

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
func (desc Description) GoString() string { return fmt.Sprintf("%s", desc.Body) }

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
