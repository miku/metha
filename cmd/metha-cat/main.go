package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	gzip "github.com/klauspost/pgzip"

	"github.com/miku/metha"
)

func main() {
	format := flag.String("format", "oai_dc", "metadata format")
	set := flag.String("set", "", "set name")
	version := flag.Bool("v", false, "show version")

	from := flag.String("from", "", "ignore records before this date")
	until := flag.String("until", "", "ignore records after this date")

	flag.Parse()

	if *version {
		fmt.Println(metha.Version)
		os.Exit(0)
	}

	if flag.NArg() == 0 {
		log.Fatal("endpoint required")
	}

	baseURL := metha.PrependSchema(flag.Arg(0))

	harvest := metha.Harvest{
		BaseURL: baseURL,
		Format:  *format,
		Set:     *set,
	}

	files, err := ioutil.ReadDir(harvest.Dir())
	if err != nil {
		log.Fatal(err)
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
