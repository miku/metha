package metha

import (
	_ "embed"
	"math/rand"
	"strings"
	"sync"
)

// RandomEndpoint returns a random endpoint url.
func RandomEndpoint() string {
	endpoints := Endpoints()
	return endpoints[rand.Intn(len(endpoints))]
}

//go:embed contrib/sites.tsv
var EndpointList string

// Endpoints from https://git.io/fxvs0.
//
// Splitting the list costs a few milliseconds and tens of megabytes, and every
// subcommand of the one binary would pay it in package init - to say nothing of
// the commands that only ever read a cache. So it happens on the first call and
// not before. The embedded string itself is free until touched: it is mapped
// from the binary rather than copied into it.
var Endpoints = sync.OnceValue(func() []string {
	return splitNonEmpty(EndpointList, "\n")
})

// splitNonEmpty is like strings.Split, except it will skip empty string
// results.
func splitNonEmpty(s string, sep string) (result []string) {
	for _, v := range strings.Split(s, sep) {
		v = strings.TrimSpace(v)
		if len(v) == 0 {
			continue
		}
		result = append(result, v)
	}
	return
}
