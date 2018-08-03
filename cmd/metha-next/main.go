package main

import (
	"fmt"
	"log"

	"github.com/miku/metha/next"
)

func main() {
	h := next.Harvest{
		Endpoint: "http://dspace.mit.edu/oai/request",
		Format:   "oai_dc",
	}
	fmt.Println(h)
	fmt.Println(h.Dir())
	desc, err := h.Description()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%v\n", desc)
	if err := h.Run(); err != nil {
		log.Fatal(err)
	}
}
