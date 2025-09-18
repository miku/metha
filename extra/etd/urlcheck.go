package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"
)

func main() {
	invert := flag.Bool("v", false, "output only invalid URLs instead of valid ones")
	flag.Parse()

	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()

	var stats = make(map[string]int)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		raw := scanner.Text()
		line := cleanURL(raw)
		if raw != line {
			stats["cleaned"]++
		}
		_, err := url.ParseRequestURI(line)
		if (*invert && err != nil) || (!*invert && err == nil) {
			_, err := fmt.Fprintf(bw, "%s\n", line)
			if err != nil {
				log.Fatal(err)
			}
			stats["valid"]++
		} else {
			stats["invalid"]++
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	b, err := json.Marshal(stats)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "%s\n", string(b))
}

func cleanURL(s string) string {
	// First extract potential URL from string
	s = extractURL(s)

	// Fix spaces around colons and slashes
	s = regexp.MustCompile(`(\w):\s+`).ReplaceAllString(s, "$1:")
	s = regexp.MustCompile(`(\w)\s+:/`).ReplaceAllString(s, "$1://")

	// Replace HTML entities
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&#xd;", "")

	// Remove trailing garbage after common URL endings
	s = regexp.MustCompile(`(com|org|net|edu|gov|io|co)[/,)\s].*$`).ReplaceAllString(s, "$1")

	// Fix common protocol mistakes (but don't double-add http)
	s = regexp.MustCompile(`(^|\s)(www\.)`).ReplaceAllString(s, "$1http://www.")
	s = strings.ReplaceAll(s, "http: //", "http://")
	s = strings.ReplaceAll(s, "https: //", "https://")
	s = strings.ReplaceAll(s, "http: <", "http://")
	s = strings.ReplaceAll(s, "http: \"", "http://")

	// Remove angle brackets if they wrap the URL
	s = strings.Trim(s, "<>")

	// Remove trailing punctuation and spaces
	s = strings.TrimRight(s, ",.;!?\")> \t\n")

	// Clean up multiple spaces
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")

	// Remove any remaining spaces from URL
	s = strings.ReplaceAll(s, " ", "")

	// Only add http:// if no protocol exists
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "http://" + s
	}

	// Remove duplicate protocols (http://http://)
	s = regexp.MustCompile(`^(https?://)+`).ReplaceAllString(s, "http://")

	return s
}

func extractURL(s string) string {
	// Try to find a URL in the string
	re := regexp.MustCompile(`(https?://[^\s<>"']+|www\.[^\s<>"']+)`)
	matches := re.FindString(s)
	if matches != "" {
		return matches
	}
	return s
}
