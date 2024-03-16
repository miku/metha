package main

import (
	"html"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/miku/parallel"
)

// url.Parse:                               1369042
// url.Parse, Trim                         91672978
// url.Parse, Trim, Unescape               91629399
// url.Parse, Trim, Unescape, ReplaceAll   91663562
//
// also: limit length
const minLength = 10
const maxLength = 120

var replacer = strings.NewReplacer(" ", "", "\t", "")

// SO: 91,663,562 URLs it is

func Scrub(p []byte) ([]byte, error) {
	s := string(p)
	s = strings.TrimSpace(s)
	s = html.UnescapeString(s)
	s = replacer.Replace(s)
	if len(s) < minLength || len(s) > maxLength {
		return nil, nil
	}
	_, err := url.Parse(s)
	if err != nil {
		return nil, nil
	}
	if !strings.Contains(s, "://") {
		return nil, nil
	}
	return []byte(s + "\n"), nil
}

func main() {
	p := parallel.NewProcessor(os.Stdin, os.Stdout, Scrub)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
