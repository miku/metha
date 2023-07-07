// Turn OAI XML stream into a simple JSON format.
//
// Previous snippets: https://archive.org/download/oai_harvest_20220921 (compat.py)

package main

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

var debug = flag.Bool("d", false, "debug output")

// Record was generated 2023-07-07 16:31:12 by tir on reka.
type Record struct {
	XMLName xml.Name `xml:"record"`
	Text    string   `xml:",chardata"`
	Xmlns   string   `xml:"xmlns,attr"`
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
			Title          []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
			} `xml:"title"`
			Creator []string `xml:"creator"`
			Subject []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
			} `xml:"subject"`
			Description []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
			} `xml:"description"`
			Publisher []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
			} `xml:"publisher"`
			Contributor []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
			} `xml:"contributor"`
			Date []string `xml:"date"`
			Type []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
			} `xml:"type"`
			Format     []string `xml:"format"`
			Identifier []string `xml:"identifier"`
			Source     []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
			} `xml:"source"`
			Language string   `xml:"language"`
			Relation []string `xml:"relation"`
			Rights   []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
			} `xml:"rights"`
			Coverage []struct {
				Text string `xml:",chardata"`
				Lang string `xml:"lang,attr"`
			} `xml:"coverage"`
		} `xml:"dc"`
	} `xml:"metadata"`
	About string `xml:"about"`
}

type TagSplitter struct {
	Tag     string
	started bool
	buf     bytes.Buffer
}

func Suffixes(s string) (suffixes []string) {
	if len(s) < 2 {
		return
	}
	for i := 1; i < len(s)-1; i++ {
		suffixes = append(suffixes, s[i:])
	}
	return
}

func (s *TagSplitter) SplitFunc(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if len(data) == 0 || atEOF {
		return 0, nil, nil
	}
	if !s.started {
		idx := bytes.Index(data, []byte("<"+s.Tag))
		if idx == -1 {
			// Check for possible suffixes, then advance a bit less
			for _, s := range Suffixes("<" + s.Tag) {
				if bytes.HasSuffix(data, []byte(s)) {
					return len(data) - len(s), nil, nil
				}
			}
			return len(data), nil, nil
		}
		s.started = true
		s.buf.Reset()
		s.buf.Write(data[idx : idx+len(s.Tag)])
		return len(data[:idx+len(s.Tag)]), nil, nil
	} else {
		idx := bytes.Index(data, []byte("</"+s.Tag+">"))
		if idx == -1 {
			// Check for possible suffixes, then advance a bit less
			for _, k := range Suffixes("</" + s.Tag + ">") {
				if bytes.HasSuffix(data, []byte(k)) {
					s.buf.Write(data[:len(data)-len(k)])
					return len(data) - len(k), nil, nil
				}
			}
			s.buf.Write(data)
			return len(data), nil, nil
		}
		s.started = false
		s.buf.Write(data[:idx+9])
		token = s.buf.Bytes()
		return len(data[:idx+9]), token, nil
	}
}

func TruncateSnippet(s string) string {
	if len(s) < 120 {
		return strings.Replace(s, "\n", " ", -1)
	}
	return fmt.Sprintf("% 10d", len(s)) + " " + strings.Replace(s[:50], "\n", "", -1) + "..." + strings.Replace(s[len(s)-50:], "\n", "", -1)
}

func main() {
	flag.Parse()
	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()
	ts := &TagSplitter{Tag: "record"}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Split(ts.SplitFunc)
	for scanner.Scan() {
		s := scanner.Text()
		if *debug {
			log.Println(TruncateSnippet(s))
		} else {
			io.WriteString(bw, s)
		}
	}
	if scanner.Err() != nil {
		log.Fatal(scanner.Err())
	}
}
