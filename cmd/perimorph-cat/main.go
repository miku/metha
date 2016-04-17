package main

import (
	"flag"
	"log"

	"github.com/miku/perimorph"
)

func main() {
	x := flag.String("x", "", "")
	flag.Parse()

	log.Printf("%s %s", perimorph.BaseDir, *x)
}
