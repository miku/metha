// Download metadata from all known endpoints (or some supplied list), generate
// a single JSON file.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

var (
	filename       = flag.String("f", "", fmt.Sprintf("filename with endpoints, defaults to list of %d sites", len(metha.Endpoints)))
	baseDir        = flag.String("base-dir", metha.GetBaseDir(), "base dir for harvested files")
	format         = flag.String("format", "oai_dc", "metadata format")
	bestEffort     = flag.Bool("B", false, "ignore harvest errors")
	maxRequests    = flag.Int("max", 1048576, "maximum number of token loops")
	quiet          = flag.Bool("q", false, "suppress all output")
	numWorkers     = flag.Int("w", runtime.NumCPU()*16, "workers")
	shuffle        = flag.Bool("S", false, "shuffle hosts")
	sample         = flag.Int("s", 0, "take a sample of endpoints (for debugging), 0 means no limit")
	seed           = flag.Int64("seed", time.Now().UTC().UnixNano(), "random seed")
	cpuprofile     = flag.String("cpuprofile", "", "cpu pprof file")
	memprofile     = flag.String("memprofile", "", "mem pprof file")
	singleEndpoint = flag.String("u", "", "use a single endpoint")

	endpoints = metha.Endpoints
)

func cleanupEndpointList(endpoints []string) (result []string) {
	for _, ep := range endpoints {
		ep = strings.TrimSpace(ep)
		if strings.HasPrefix(ep, "#") || strings.HasPrefix(ep, "//") || ep == "" {
			continue
		}
		result = append(result, ep)
	}
	return result
}

func main() {
	flag.Parse()
	rand.Seed(*seed)
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}
	if *filename != "" {
		b, err := ioutil.ReadFile(*filename)
		if err != nil {
			log.Fatal(err)
		}
		endpoints = strings.Split(string(b), "\n")
	}
	if *singleEndpoint != "" {
		endpoints = []string{*singleEndpoint}
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
	endpoints = cleanupEndpointList(endpoints)
	// Run and wait until all harvests are done. XXX: add some timeout option.
	var (
		g    = new(errgroup.Group)
		urlC = make(chan string)
	)
	// Enqueue tasks.
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
				log.Printf("w@%d", j)
				harvest, err := metha.NewHarvest(u)
				if err != nil {
					log.Printf("failed (init): %s, %v", u, err)
					continue
				}
				harvest.MaxRequests = *maxRequests
				harvest.CleanBeforeDecode = true
				harvest.Format = *format
				if err = harvest.Run(); err != nil {
					switch err {
					case metha.ErrAlreadySynced:
					default:
						// fall back to non-selective mode
						harvest.DisableSelectiveHarvesting = true
						if err = harvest.Run(); err != nil {
							log.Printf("failed (harvest): %s, %v", u, err)
							continue
						}
					}
				}
			}
			return nil
		})
	}
	err := g.Wait()
	if err != nil {
		log.Fatal(err)
	}
	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()
	for _, u := range endpoints {
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
			if *bestEffort {
				log.Printf("error rendering endpoint %v: %v", u, err)
				continue
			}
			log.Fatal(err)
		}
	}
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		runtime.GC()    // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("could not write memory profile: ", err)
		}
	}
}
