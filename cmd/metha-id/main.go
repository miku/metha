package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
)

func main() {
	version := flag.Bool("v", false, "show version")

	flag.Parse()

	if *version {
		fmt.Println(metha.Version)
		os.Exit(0)
	}

	if flag.NArg() == 0 {
		log.Fatalf("An endpoint URL is required, maybe try: %s", metha.RandomEndpoint())
	}

	baseURL := metha.PrependSchema(flag.Arg(0))
	repo := metha.Repository{BaseURL: baseURL}

	m := make(map[string]interface{})

	req := metha.Request{Verb: "Identify", BaseURL: baseURL}
	resp, err := metha.StdClient.Do(&req)
	if err != nil {
		log.Fatal(err)
	}
	m["identify"] = resp.Identify

	if formats, err := repo.Formats(); err == nil {
		m["formats"] = formats
	} else {
		log.Println(err)
	}
	if sets, err := repo.Sets(); err == nil {
		m["sets"] = sets
	} else {
		log.Println(err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(m); err != nil {
		log.Fatal(err)
	}
	fmt.Println()
}
