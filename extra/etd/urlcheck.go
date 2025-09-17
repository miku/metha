package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
)

func main() {
	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()
	var (
		scanner = bufio.NewScanner(os.Stdin)
		stats   = make(map[string]int)
	)
	for scanner.Scan() {
		line := scanner.Text()
		if _, err := url.ParseRequestURI(line); err == nil {
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
