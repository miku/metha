package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/miku/metha"
	"github.com/miku/metha/store"
	log "github.com/sirupsen/logrus"
)

var (
	showAll    = flag.Bool("a", false, "show full path")
	bestEffort = flag.Bool("b", false, "continue in the presence of errors")
)

func ellipsis(s string, length int) string {
	if len(s) > length {
		return s[:length] + "..."
	}
	return s
}

func main() {
	flag.Parse()
	for entry, err := range store.List(metha.GetBaseDir()) {
		if err != nil {
			if *bestEffort {
				log.Println(err)
				continue
			}
			log.Fatal(err)
		}
		name := filepath.Base(entry.Dir)
		if !*showAll {
			name = ellipsis(name, 35)
		}
		id := entry.Identity
		fmt.Printf("%s\t%s\t%s\t%s\n", name, id.Set, id.Format, id.BaseURL)
	}
}
