package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	gzip "github.com/klauspost/pgzip"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
)

var (
	format  = flag.String("format", "oai_dc", "metadata format")
	set     = flag.String("set", "", "set name")
	version = flag.Bool("v", false, "show version")
	baseDir = flag.String("base-dir", metha.GetBaseDir(), "base dir for harvested files")
	from    = flag.String("from", "", "ignore records before this date")
	until   = flag.String("until", "", "ignore records after this date")
	root    = flag.String("root", "Records", "root element to wrap records into")
)

func main() {

	flag.Parse()

	if *version {
		fmt.Println(metha.Version)
		os.Exit(0)
	}

	if flag.NArg() == 0 {
		log.Fatal("endpoint required")
	}

	baseURL := metha.PrependSchema(flag.Arg(0))

	metha.BaseDir = *baseDir
	harvest := metha.Harvest{
		BaseURL: baseURL,
		Format:  *format,
		Set:     *set,
	}

	files, err := ioutil.ReadDir(harvest.Dir())
	if err != nil {
		// Fallback to fragment of base URL, e.g. allow "metha-cat xyz", if xyz
		// is not ambiguous.
		candidates, err := metha.FindRepositoriesByString(flag.Arg(0))
		if err != nil {
			log.Fatal(err)
		}
		if len(candidates) == 0 {
			log.Fatal("not an endpoint url nor fragment")
		}
		if len(candidates) > 1 {
			log.Fatalf("ambiguous fragment %v matches %d values: %v",
				flag.Arg(0),
				len(candidates),
				strings.Join(candidates, ", "),
			)
		}
		// It is a bit irritating to fallback to the same URL, so only log, if
		// there's actually a difference.
		if candidates[0] != harvest.BaseURL {
			log.Printf("falling back from %s to %s", harvest.BaseURL, candidates[0])
		}
		harvest.BaseURL = candidates[0]

		files, err = ioutil.ReadDir(harvest.Dir())
		if err != nil {
			log.Fatal(err)
		}
	}

	if *root != "" {
		fmt.Printf("<%s xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">\n", *root)
		defer fmt.Printf("</%s>\n", *root)
	}

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".xml.gz") {
			continue
		}

		if *from != "" && file.Name() < *from {
			continue
		}

		abspath := filepath.Join(harvest.Dir(), file.Name())

		fi, err := os.Open(abspath)
		if err != nil {
			log.Fatal(err)
		}
		r, err := gzip.NewReader(fi)
		if err != nil {
			log.Fatal(err)
		}

		dec := xml.NewDecoder(r)
		dec.Strict = false

		var resp metha.Response
		if err := dec.Decode(&resp); err != nil {
			log.Fatal(err)
		}

		for _, rec := range resp.ListRecords.Records {
			if *from != "" && rec.Header.DateStamp < *from {
				continue
			}
			if *until != "" && rec.Header.DateStamp > *until {
				continue
			}

			b, err := xml.Marshal(rec)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(string(b))
		}
	}
}
