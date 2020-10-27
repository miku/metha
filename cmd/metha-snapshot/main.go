// Download all known endpoints, generate a single JSON file.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

var (
	filename    = flag.String("f", "", fmt.Sprintf("filename with endpoints, defaults to list of %d sites", len(metha.Endpoints)))
	baseDir     = flag.String("base-dir", metha.GetBaseDir(), "base dir for harvested files")
	format      = flag.String("format", "oai_dc", "metadata format")
	maxRequests = flag.Int("max", 1048576, "maximum number of token loops")
	quiet       = flag.Bool("q", false, "suppress all output")
	numWorkers  = flag.Int("w", 64, "workers")
	shuffle     = flag.Bool("S", false, "shuffle hosts")
	sample      = flag.Int("s", 0, "take a sample of endpoints (for debugging), 0 means no limit")
	seed        = flag.Int64("seed", time.Now().UTC().UnixNano(), "random seed")
)

func main() {
	flag.Parse()
	rand.Seed(*seed)
	var (
		endpoints = metha.Endpoints
		failed    []string
	)
	if *filename != "" {
		b, err := ioutil.ReadFile(*filename)
		if err != nil {
			log.Fatal(err)
		}
		endpoints = strings.Split(string(b), "\n")
	}
	if *shuffle {
		rand.Shuffle(len(endpoints), func(i, j int) {
			endpoints[i], endpoints[j] = endpoints[j], endpoints[i]
		})
	}
	if *sample > 0 {
		if len(endpoints) > *sample {
			endpoints = endpoints[:*sample]
		}
	}
	if *quiet {
		log.SetOutput(ioutil.Discard)
	}
	// Run and wait until all harvests are done. XXX: add some timeout option.
	g := new(errgroup.Group)
	urlC := make(chan string)
	g.Go(func() error {
		defer close(urlC)
		for _, endpoint := range endpoints {
			urlC <- endpoint
		}
		return nil
	})
	for i := 0; i < *numWorkers; i++ {
		g.Go(func() error {
			var j int
			for u := range urlC {
				j++
				harvest, err := metha.NewHarvest(u)
				if err != nil {
					failed = append(failed, u)
					log.Printf("failed (init): %s, %v", u, err)
					continue
				}
				harvest.MaxRequests = *maxRequests
				harvest.CleanBeforeDecode = true
				harvest.Format = *format
				if err := harvest.Run(); err != nil {
					switch err {
					case metha.ErrAlreadySynced:
					default:
						// Fall back to non-window mode.
						harvest.DisableSelectiveHarvesting = true
						if err := harvest.Run(); err != nil {
							failed = append(failed, u)
							log.Printf("failed (harvest): %s, %v", u, err)
							continue
						}
					}
				}
			}
			return nil
		})
	}
	g.Wait()
	for _, f := range failed {
		log.Println(f)
	}
	failedSet := make(map[string]struct{})
	for _, f := range failed {
		failedSet[f] = struct{}{}
	}
	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()
	for _, u := range endpoints {
		if _, ok := failedSet[u]; ok {
			continue
		}
		metha.BaseDir = *baseDir
		harvest := metha.Harvest{
			BaseURL: u,
			Format:  *format,
		}
		opts := &metha.RenderOpts{
			Writer:  bw,
			Harvest: harvest,
			UseJson: true,
		}
		if err := metha.Render(opts); err != nil {
			log.Fatal(err)
		}
	}
}
