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
var Endpoints = strings.Split(EndpointList, "\n")
