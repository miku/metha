// genjson extracts info from a stream of OAI DC XML records, e.g.
//
//	<record><header>...
//
//	<dc:language>eng</dc:language>
//	<dc:relation>https://ejournal.uksw.edu/ijpna/article/view/1351/731</dc:relation>
//	<dc:rights xml:lang="en-US">Copyright (c) 2017 Indonesian Journal of Physics and Nuclear Applications</dc:rights>
//	<dc:rights xml:lang="en-US">http://creativecommons.org/licenses/by-nc-nd/4.0</dc:rights>
//	</oai_dc:dc>
//	</metadata><about></about></record>
//
//
//	<record> ...
//
// Run like:
//
//	$ sed -e 's@<record>@\n\n\n<record>@' oai.data | python genrecords.py | go run genjson.go
//
// Note that the input does not need to be valid XML, but rather each record
// element needs to be followed by two lines with only newlines (as separator).
//
// Outputs a converted JSON lines stream to stdout. The JSON will contain
// parsed issn, url and DOI. Example output:
//
//	{
//	  "oai": "oai:ejournal.uksw.edu:article/1673",
//	  "datestamp": "2018-05-16T01:48:17Z",
//	  "sets": [
//	    "ijpna:ART",
//	    "driver"
//	  ],
//	  "creators": [
//	    "Sardjono, Yohannes",
//	    "Kusminarto, Kusminarto",
//	    "Wusko, Ikna Urwatul"
//	  ],
//	  "doi": [
//	    "10.24246/ijpna.v3i1.29-35"
//	  ],
//	  "formats": [
//	    "application/pdf"
//	  ],
//	  "issn": [
//	    "2550-0570",
//	    "2549-046X"
//	  ],
//	  "ids": [
//	    "https://ejournal.uksw.edu/ijpna/article/view/1673",
//	    "10.24246/ijpna.v3i1.29-35"
//	  ],
//	  "languages": [
//	    "eng"
//	  ],
//	  "urls": [
//	    "https://ejournal.uksw.edu/ijpna/article/view/1673"
//	  ],
//	  "publishers": [
//	    "Fakultas Sains dan Matematika Universitas Kristen Satya Wacana"
//	  ],
//	  "relations": [
//	    "https://ejournal.uksw.edu/ijpna/article/view/1673/894"
//	  ],
//	  "rights": [
//	    "Copyright (c) 2018 Indonesian Journal of Physics and Nuclear Applications",
//	    "http://creativecommons.org/licenses/by/4.0"
//	  ],
//	  "titles": [
//	    "The Optimization of Collimator Material and In Vivo Testing Dosimetry of Boron Neutron Capture Therapy (BNCT) on Radial Piercing Beam Port Kartini Nuclear Reactor by Monte Carlo N-Particle Extended (MCNPX) Simulation Method"
//	  ],
//	  "types": [
//	    "info:eu-repo/semantics/article",
//	    "info:eu-repo/semantics/publishedVersion",
//	    "Peer-reviewed Article"
//	  ]
//	}
//
// Note: it takes about 5 hours to generate a list of
package main

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
)

var (
	bestEffort = flag.Bool("b", false, "do not fail on XML errors")

	doiRe  = regexp.MustCompile(`10[.][0-9]{1,8}/[^ ]*`)
	issnRe = regexp.MustCompile(`[0-9]{4,4}-?[0-9]{3,3}[0-9xX]`)
)

func main() {
	flag.Parse()
	var (
		br    = bufio.NewReader(os.Stdin)
		batch []string
		blob  string
	)
	for {
		line, err := br.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if l := len(batch); l > 2 && (batch[l-1] == "\n" && batch[l-2] == "\n" && strings.Contains(batch[l-3], "</record>")) {
			blob = strings.Join(batch, "")
			info, err := extractInfo([]byte(blob))
			if err != nil {
				if *bestEffort {
					log.Printf("%v (%v)", err, blob)
					batch = batch[:0]
					continue
				} else {
					log.Fatal(err)
				}
			}
			b, err := json.Marshal(info)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(string(b))
			batch = batch[:0]
		}
		batch = append(batch, line)
	}
}

// Record was generated 2020-03-17 16:11:30 by tir on trieste.
type Record struct {
	XMLName xml.Name `xml:"record"`
	Text    string   `xml:",chardata"`
	Header  struct {
		Text       string   `xml:",chardata"`
		Status     string   `xml:"status,attr"`
		Identifier string   `xml:"identifier"`
		Datestamp  string   `xml:"datestamp"`
		SetSpec    []string `xml:"setSpec"`
	} `xml:"header"`
	Metadata struct {
		Text string `xml:",chardata"`
		Dc   struct {
			Text           string `xml:",chardata"`
			OaiDc          string `xml:"oai_dc,attr"`
			Dc             string `xml:"dc,attr"`
			Xsi            string `xml:"xsi,attr"`
			SchemaLocation string `xml:"schemaLocation,attr"`
			Doc            string `xml:"doc,attr"`
			Xmlns          string `xml:"xmlns,attr"`
			Cm             string `xml:"cm,attr"`
			Cs             string `xml:"cs,attr"`
			Spectrum       string `xml:"spectrum,attr"`
			Ns2            string `xml:"ns2,attr"`
			Title          []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
				Dc   string `xml:"dc,attr"`
				Sub  string `xml:"sub"`
			} `xml:"title"`
			Creator []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
				Dc   string `xml:"dc,attr"`
				ID   string `xml:"id,attr"`
			} `xml:"creator"`
			Description []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
				Dc   string `xml:"dc,attr"`
			} `xml:"description"`
			Publisher []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
				Dc   string `xml:"dc,attr"`
			} `xml:"publisher"`
			Date []struct {
				Text string `xml:",chardata"`
				Dc   string `xml:"dc,attr"`
				Lang string `xml:"lang,attr"`
			} `xml:"date"`
			Type []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
				Dc   string `xml:"dc,attr"`
			} `xml:"type"`
			Format []struct {
				Text string `xml:",chardata"`
				Dc   string `xml:"dc,attr"`
				Lang string `xml:"lang,attr"`
			} `xml:"format"`
			Identifier []struct {
				Text   string `xml:",chardata"`
				Dc     string `xml:"dc,attr"`
				Jtitle string `xml:"jtitle"`
			} `xml:"identifier"`
			Source []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
			} `xml:"source"`
			Language []struct {
				Text string `xml:",chardata"`
				Dc   string `xml:"dc,attr"`
				Lang string `xml:"lang,attr"`
			} `xml:"language"`
			Relation []struct {
				Text string `xml:",chardata"`
				Dc   string `xml:"dc,attr"`
				Lang string `xml:"lang,attr"`
			} `xml:"relation"`
			Rights []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
				Dc   string `xml:"dc,attr"`
			} `xml:"rights"`
			Contributor []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
				Dc   string `xml:"dc,attr"`
			} `xml:"contributor"`
			Subject []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
				Dc   string `xml:"dc,attr"`
			} `xml:"subject"`
			Coverage []struct {
				Text     string `xml:",chardata"`
				Lang     string `xml:"lang,attr"`
				Resource string `xml:"resource,attr"`
			} `xml:"coverage"`
			IdentifierURIFulltext []string `xml:"identifier.uri.fulltext"`
			Audience              struct {
				Text string `xml:",chardata"`
				Dc   string `xml:"dc,attr"`
			} `xml:"audience"`
			Doi    string `xml:"doi"`
			Extent string `xml:"extent"`
		} `xml:"dc"`
	} `xml:"metadata"`
	About string `xml:"about"`
}

// Info is some information out of OAI raw XML.
type Info struct {
	OAI       string   `json:"oai,omitempty"`
	Status    string   `json:"status,omitempty"`
	Datestamp string   `json:"datestamp,omitempty"`
	Sets      []string `json:"sets,omitempty"`

	Contributors          []string `json:"contributors,omitempty"`
	Coverage              []string `json:"coverage,omitempty"`
	Creators              []string `json:"creators,omitempty"`
	Descriptions          []string `json:"descriptions,omitempty"`
	DOI                   []string `json:"doi,omitempty"`
	Dates                 []string `json:"dates,omitempty"`
	Formats               []string `json:"formats,omitempty"`
	ISSN                  []string `json:"issn,omitempty"`
	IdentifierURIFulltext []string `json:"fulltext_uri,omitempty"`
	Identifiers           []string `json:"ids,omitempty"`
	Languages             []string `json:"languages,omitempty"`
	Links                 []string `json:"urls,omitempty"`
	Publishers            []string `json:"publishers,omitempty"`
	Relations             []string `json:"relations,omitempty"`
	Rights                []string `json:"rights,omitempty"`
	Sources               []string `json:"sources,omitempty"`
	Subjects              []string `json:"subjects,omitempty"`
	Titles                []string `json:"titles,omitempty"`
	Types                 []string `json:"types,omitempty"`
}

// extractInfo extracts various bits and pieces from a record.
func extractInfo(p []byte) (*Info, error) {
	var record Record
	if err := xml.Unmarshal(p, &record); err != nil {
		return nil, err
	}
	dc := record.Metadata.Dc

	// Some things we would get out.
	var contributors, coverage, creators, descriptions, formats, dois, ids, issns,
		languages, publishers, rels, rights, sources, subjects, titles, types, urls []string

	for _, v := range dc.Contributor {
		if v.Text == "" {
			continue
		}
		contributors = appendUnique(contributors, v.Text)
	}
	for _, v := range dc.Coverage {
		if v.Text == "" {
			continue
		}
		coverage = appendUnique(coverage, v.Text)
	}
	for _, v := range dc.Creator {
		if v.Text == "" {
			continue
		}
		creators = appendUnique(creators, v.Text)
	}
	for _, v := range dc.Description {
		if v.Text == "" {
			continue
		}
		descriptions = appendUnique(descriptions, v.Text)
	}
	for _, v := range dc.Format {
		if v.Text == "" {
			continue
		}
		formats = appendUnique(formats, v.Text)
	}
	for _, v := range dc.Identifier {
		if v.Text == "" {
			continue
		}
		ids = appendUnique(ids, v.Text)
	}
	for _, v := range dc.Language {
		if v.Text == "" {
			continue
		}
		languages = appendUnique(languages, v.Text)
	}
	for _, v := range dc.Publisher {
		if v.Text == "" {
			continue
		}
		publishers = appendUnique(publishers, v.Text)
	}
	for _, v := range dc.Rights {
		if v.Text == "" {
			continue
		}
		rights = appendUnique(rights, v.Text)
	}
	for _, v := range dc.Source {
		if v.Text == "" {
			continue
		}
		sources = appendUnique(sources, v.Text)
	}
	for _, v := range dc.Subject {
		if v.Text == "" {
			continue
		}
		subjects = appendUnique(subjects, v.Text)
	}
	for _, v := range dc.Type {
		if v.Text == "" {
			continue
		}
		types = appendUnique(types, v.Text)
	}
	for _, v := range dc.Relation {
		if v.Text == "" {
			continue
		}
		rels = appendUnique(rels, v.Text)
	}
	for _, v := range dc.Title {
		if v.Text == "" {
			continue
		}
		titles = appendUnique(titles, v.Text)
	}
	if dc.Doi != "" {
		dois = appendUnique(dois, dc.Doi)
	}
	// Find URL, DOI, ISSN, and other structured data.
	for _, v := range ids {
		switch {
		case strings.HasPrefix(v, "http"):
			urls = appendUnique(urls, v)
		case doiRe.MatchString(v):
			dois = appendUnique(dois, doiRe.FindString(v))
		case issnRe.MatchString(v):
			issns = appendUnique(issns, issnRe.FindString(v))
		}
	}
	for _, v := range sources {
		switch {
		case strings.HasPrefix(v, "http"):
			urls = appendUnique(urls, v)
		case doiRe.MatchString(v):
			dois = appendUnique(dois, doiRe.FindString(v))
		case issnRe.MatchString(v):
			issns = appendUnique(issns, issnRe.FindString(v))
		}
	}
	info := Info{
		OAI:                   record.Header.Identifier,
		Datestamp:             record.Header.Datestamp,
		Sets:                  record.Header.SetSpec,
		Status:                record.Header.Status,
		Contributors:          contributors,
		Coverage:              coverage,
		Creators:              creators,
		DOI:                   dois,
		Formats:               formats,
		ISSN:                  issns,
		IdentifierURIFulltext: dc.IdentifierURIFulltext,
		Identifiers:           ids,
		Languages:             languages,
		Links:                 urls,
		Publishers:            publishers,
		Relations:             rels,
		Rights:                rights,
		Subjects:              subjects,
		Titles:                titles,
		Types:                 types,
	}
	return &info, nil
}

func appendUnique(ss []string, v string) []string {
	for _, s := range ss {
		if s == v {
			return ss
		}
	}
	ss = append(ss, v)
	return ss
}
