package cli

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/miku/metha"
	"github.com/miku/metha/oai"
	"github.com/neurosnap/sentences/english"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newFortuneCmd() *cobra.Command {
	var (
		debug           bool
		k               int
		timeout         time.Duration
		sentence        bool
		oneSentenceOnly bool
	)
	cmd := &cobra.Command{
		Use:     "fortune",
		Short:   "Print a random record description from a random endpoint",
		Args:    cobra.NoArgs,
		Aliases: []string{"metha-fortune"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !debug {
				log.SetOutput(io.Discard)
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			var searchers []search
			for i := 0; i < k; i++ {
				searchers = append(searchers, createSearcher(metha.RandomEndpoint(), debug, sentence || oneSentenceOnly))
			}
			s := spinner.New(spinner.CharSets[25], 100*time.Millisecond)
			s.Writer = os.Stderr
			if !debug {
				s.Start()
			}
			result := first(ctx, searchers...)
			if !debug {
				s.Stop()
			}
			if result.Err != nil || result.Fortune == "" {
				fmt.Printf("No fortune available at this time.\n")
				if debug {
					log.Printf("%v", result.Err)
				}
				os.Exit(1)
			}
			fmt.Println(result.Fortune)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&debug, "debug", "d", false, "debug output")
	f.IntVarP(&k, "parallel", "k", 16, "number of endpoints to query in parallel")
	f.DurationVarP(&timeout, "timeout", "t", 8*time.Second, "timeout")
	f.BoolVarP(&sentence, "sentence", "s", false, "only one sentence (deprecated)")
	f.BoolVarP(&oneSentenceOnly, "one-sentence", "1", false, "one sentence only")
	return cmd
}

// dc was generated 2018-05-10 14:57:24 by tir on sol.
type dc struct {
	XMLName        xml.Name `xml:"dc"`
	Text           string   `xml:",chardata"`
	OaiDc          string   `xml:"oai_dc,attr"`
	Dc             string   `xml:"dc,attr"`
	Xsi            string   `xml:"xsi,attr"`
	SchemaLocation string   `xml:"schemaLocation,attr"`
	Title          []struct {
		Text string `xml:",chardata"`
		Lang string `xml:"lang,attr"`
	} `xml:"title"`
	Creator []struct {
		Text string `xml:",chardata"`
	} `xml:"creator"`
	Description []struct {
		Text string `xml:",chardata"`
		Lang string `xml:"lang,attr"`
	} `xml:"description"`
	Publisher []struct {
		Text string `xml:",chardata"`
		Lang string `xml:"lang,attr"`
	} `xml:"publisher"`
	Date struct {
		Text string `xml:",chardata"`
	} `xml:"date"`
	Type []struct {
		Text string `xml:",chardata"`
		Lang string `xml:"lang,attr"`
	} `xml:"type"`
	Format struct {
		Text string `xml:",chardata"`
	} `xml:"format"`
	Identifier struct {
		Text string `xml:",chardata"`
	} `xml:"identifier"`
	Source []struct {
		Text string `xml:",chardata"`
		Lang string `xml:"lang,attr"`
	} `xml:"source"`
	Language struct {
		Text string `xml:",chardata"`
	} `xml:"language"`
	Relation struct {
		Text string `xml:",chardata"`
	} `xml:"relation"`
	Rights []struct {
		Text string `xml:",chardata"`
		Lang string `xml:"lang,attr"`
	} `xml:"rights"`
}

type fortuneResult struct {
	Fortune string
	Err     error
}

type search func(ctx context.Context) fortuneResult

// first returns the first search that comes back with something to print.
func first(ctx context.Context, endpoints ...search) fortuneResult {
	c := make(chan fortuneResult, len(endpoints))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, ep := range endpoints {
		go func(endpoint search) { c <- endpoint(ctx) }(ep)
	}
	for {
		select {
		case <-ctx.Done():
			return fortuneResult{Err: ctx.Err()}
		case r := <-c:
			if r.Err == nil && len(r.Fortune) > 0 {
				return r
			}
			log.Printf("backend returned with an error or an empty description: %v", r.Err)
		}
	}
}

// createSearcher assembles a search type.
func createSearcher(endpoint string, debug, oneSentence bool) search {
	return func(ctx context.Context) fortuneResult {
		client := oai.CreateClient(8*time.Second, 3)
		req := oai.Request{
			BaseURL:        endpoint,
			Verb:           "ListIdentifiers",
			MetadataPrefix: "oai_dc",
		}
		resp, err := client.Do(&req)
		if err != nil {
			return fortuneResult{Err: err}
		}
		var ids []string
		for _, h := range resp.ListIdentifiers.Headers {
			ids = append(ids, h.Identifier)
		}
		if len(ids) == 0 {
			return fortuneResult{Err: fmt.Errorf("no identifiers found")}
		}
		if debug {
			events := len(ids) * len(metha.Endpoints())
			log.Printf("estimated probability of record: 1/%d", events)
		}
		rid := ids[rand.Intn(len(ids))]
		req = oai.Request{
			BaseURL:        endpoint,
			Verb:           "GetRecord",
			MetadataPrefix: "oai_dc",
			Identifier:     rid,
		}
		resp, err = client.Do(&req)
		if err != nil {
			return fortuneResult{Err: err}
		}
		if len(resp.GetRecord.Record.Metadata.Body) == 0 {
			return fortuneResult{Err: fmt.Errorf("empty metadata body")}
		}
		var record dc
		dec := xml.NewDecoder(bytes.NewReader(resp.GetRecord.Record.Metadata.Body))
		dec.Strict = false
		if err := dec.Decode(&record); err != nil {
			return fortuneResult{Err: fmt.Errorf("XML decode error: %w", err)}
		}
		if len(record.Description) == 0 {
			return fortuneResult{Err: fmt.Errorf("no descriptions")}
		}
		text := html.UnescapeString(strings.TrimSpace(record.Description[0].Text))
		if len(text) == 0 {
			return fortuneResult{Err: fmt.Errorf("empty description")}
		}
		var buf bytes.Buffer
		if oneSentence {
			tokenizer, err := english.NewSentenceTokenizer(nil)
			if err != nil {
				log.Println(err)
				io.WriteString(&buf, text)
			}
			sentences := tokenizer.Tokenize(text)
			if len(sentences) > 0 {
				io.WriteString(&buf, sentences[0].Text)
			} else {
				io.WriteString(&buf, text)
			}
		} else {
			io.WriteString(&buf, text)
		}
		fmt.Fprintf(&buf, "\n\n    -- %s", endpoint)
		return fortuneResult{Fortune: buf.String()}
	}
}
