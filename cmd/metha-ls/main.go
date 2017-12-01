package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	"github.com/miku/metha"
)

func ellipsis(s string, length int) string {
	if len(s) > length {
		return s[:length] + "..."
	}
	return s
}

func main() {
	showAll := flag.Bool("a", false, "show full path")
	bestEffort := flag.Bool("b", false, "continue in the presence of errors")
	flag.Parse()

	files, err := ioutil.ReadDir(metha.BaseDir)
	if err != nil {
		log.Fatal(err)
	}

	for _, file := range files {
		b, err := base64.RawURLEncoding.DecodeString(file.Name())
		if err != nil {
			if *bestEffort {
				log.Println(err)
			} else {
				log.Fatal(err)
			}
		}

		parts := strings.SplitN(string(b), "#", 3)
		if len(parts) < 3 {
			continue
		}
		if *showAll {
			fmt.Printf("%s\t%s\n", file.Name(), strings.Join(parts, "\t"))
		} else {
			fmt.Printf("%s\t%s\n", ellipsis(file.Name(), 35), strings.Join(parts, "\t"))
		}

	}
}
