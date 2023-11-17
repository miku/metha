package metha

import (
	_ "embed"
	"math/rand"
	"strings"
)

// RandomEndpoint returns a random endpoint url.
func RandomEndpoint() string {
	return Endpoints[rand.Intn(len(Endpoints))]
}

//go:embed contrib/sites.tsv
var EndpointList string

// Endpoints from https://git.io/fxvs0.
var Endpoints = splitNonEmpty(EndpointList, "\n")

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
