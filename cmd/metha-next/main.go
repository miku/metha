package main

import (
	"github.com/miku/metha/next"
	log "github.com/sirupsen/logrus"
)

func main() {
	h := next.Harvest{
		Endpoint: "http://dspace.mit.edu/oai/request",
		Format:   "oai_dc",
	}

	log.Println(h)
	log.Println(h.Dir())

	desc, err := h.Description()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%v", desc)
	if err := h.Run(); err != nil {
		log.Fatal(err)
	}

	files, err := h.Files()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("this harvest contains %d file(s)", len(files))
}
