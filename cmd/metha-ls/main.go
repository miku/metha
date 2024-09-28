package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/miku/metha"
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
	basedir := metha.GetBaseDir()
	if _, err := os.Stat(basedir); os.IsNotExist(err) {
		return
	}
	files, err := ioutil.ReadDir(metha.GetBaseDir())
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
		name := ellipsis(file.Name(), 35)
		if *showAll {
			name = file.Name()
		}
		fmt.Printf("%s\t%s\n", name, strings.Join(parts, "\t"))
	}
}
