package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
)

// Dc was generated 2018-05-10 14:57:24 by tir on sol.
type Dc struct {
	XMLName        xml.Name `xml:"dc"`
	Text           string   `xml:",chardata"`
	OaiDc          string   `xml:"oai_dc,attr"`
	Dc             string   `xml:"dc,attr"`
	Xsi            string   `xml:"xsi,attr"`
	SchemaLocation string   `xml:"schemaLocation,attr"`
	Title          []struct {
		Text string `xml:",chardata"` // The contribution of thesa...
		Lang string `xml:"lang,attr"`
	} `xml:"title"`
	Creator []struct {
		Text string `xml:",chardata"` // Casari Boccato, Vera Regi...
	} `xml:"creator"`
	Description []struct {
		Text string `xml:",chardata"` // From the interdisciplinar...
		Lang string `xml:"lang,attr"`
	} `xml:"description"`
	Publisher []struct {
		Text string `xml:",chardata"` // Ibersid: journal of infor...
		Lang string `xml:"lang,attr"`
	} `xml:"publisher"`
	Date struct {
		Text string `xml:",chardata"` // 2008-09-15
	} `xml:"date"`
	Type []struct {
		Text string `xml:",chardata"` // info:eu-repo/semantics/ar...
		Lang string `xml:"lang,attr"`
	} `xml:"type"`
	Format struct {
		Text string `xml:",chardata"` // application/pdf
	} `xml:"format"`
	Identifier struct {
		Text string `xml:",chardata"` // https://ibersid.eu/ojs/in...
	} `xml:"identifier"`
	Source []struct {
		Text string `xml:",chardata"` // Ibersid: journal of infor...
		Lang string `xml:"lang,attr"`
	} `xml:"source"`
	Language struct {
		Text string `xml:",chardata"` // spa
	} `xml:"language"`
	Relation struct {
		Text string `xml:",chardata"` // https://ibersid.eu/ojs/in...
	} `xml:"relation"`
	Rights []struct {
		Text string `xml:",chardata"` // © 2007-present Francisco...
		Lang string `xml:"lang,attr"`
	} `xml:"rights"`
}

func main() {
	rand.Seed(time.Now().UnixNano())
	log.SetOutput(ioutil.Discard)
	maxEndpoints := 4

	client := metha.CreateClient(5*time.Second, 3)

	for i := 0; i < maxEndpoints; i++ {

		s := spinner.New(spinner.CharSets[25], 100*time.Millisecond)

		s.Writer = os.Stderr
		s.Start()

		ep := metha.RandomEndpoint()

		req := metha.Request{
			BaseURL:        ep,
			Verb:           "ListIdentifiers",
			MetadataPrefix: "oai_dc",
		}
		resp, err := client.Do(&req)
		if err != nil {
			s.Stop()
			continue
		}
		var ids []string
		for _, h := range resp.ListIdentifiers.Headers {
			ids = append(ids, h.Identifier)
		}
		if len(ids) == 0 {
			s.Stop()
			continue
		}
		rid := ids[rand.Intn(len(ids))]

		req = metha.Request{
			BaseURL:        ep,
			Verb:           "GetRecord",
			MetadataPrefix: "oai_dc",
			Identifier:     rid,
		}
		resp, err = client.Do(&req)
		if err != nil {
			s.Stop()
			continue
		}

		var record Dc
		dec := xml.NewDecoder(bytes.NewReader(resp.GetRecord.Record.Metadata.Body))
		dec.Strict = false
		if err := dec.Decode(&record); err != nil {
			s.Stop()
			continue
		}
		if len(record.Description) > 0 {
			s.Stop()
			if len(record.Description[0].Text) == 0 {
				continue
			}
			fmt.Println(record.Description[0].Text)
			fmt.Println()
			fmt.Printf("    -- %s\n", ep)
		}

		os.Exit(0)
	}
	fmt.Println("No fortunes available at this time.")
	os.Exit(1)
}
