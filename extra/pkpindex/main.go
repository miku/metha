// Small util to get journal info from https://index.pkp.sfu.ca currently
// including 1264043 records indexed from 4960 publications.
//
// https://pkp.sfu.ca/2015/10/23/introducing-the-pkp-index/
//
// Notes.
//
// Index page will not yield 404 on invalid page, so max page needs to be set
// manually for now. Pagination seems to require more, maybe cookies.
//
// Pagination is broken, direct link, with custom UA, cookie ends always ends
// up at first page; probably a bit too much JS.
//
// Fetch each journal info page, e.g.
// https://index.pkp.sfu.ca/index.php/browse/archiveInfo/5421 - non-existent
// pages will redirect to homepage, but not via HTTP 3XX, but via "refresh"
// header (http://www.otsukare.info/2015/03/26/refresh-http-header).
//
// Certainly, a site with character.
//
// <div id="content">
// <h3>Revista de Psicologia del Deporte</h3>
// <p class="archiveLinks"><a
// href="https://index.pkp.sfu.ca/index.php/browse/index/37">Browse
// Records</a>&nbsp;&nbsp;|&nbsp;&nbsp;<a href="http://rpd-online.com"
// target="_blank">Journal Website</a>&nbsp;&nbsp;|&nbsp;&nbsp;<a
// href="http://rpd-online.com/issue/current" target="_blank">Current
// Issue</a>&nbsp;&nbsp;|&nbsp;&nbsp;<a
// href="http://rpd-online.com/issue/archive" target="_blank">All
// Issues</a></p>
//
// Let's https://github.com/ericchiang/pup
//
// cat page-000281.html | pup 'h3 text{}' # Journal of Modern Materials
// cat page-000281.html | pup 'p.archiveLinks > a:nth-child(2) attr{href}' # https://journals.aijr.in/index.php/jmm/index
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/sethgrid/pester"
)

const appName = "pkpindex"

var (
	cacheDir  = flag.String("d", path.Join(xdg.CacheHome, appName), "path to cache dir")
	tag       = flag.String("t", time.Now().Format("2006-01-02"), "subdirectory under cache dir to store pages")
	baseURL   = flag.String("b", "https://index.pkp.sfu.ca/index.php/browse", "base url")
	sleep     = flag.Duration("s", 1*time.Second, "sleep between requests")
	verbose   = flag.Bool("verbose", false, "verbose output")
	maxID     = flag.Int("x", 7000, "upper bound, exclusive; max id to fetch")
	userAgent = flag.String("ua", "Mozilla/5.0 (compatible; MSIE 9.0; Windows NT 6.1; Trident/5.0)", "user agent to use")
	force     = flag.Bool("f", false, "force redownload of zero length files")
)

type JournalInfo struct {
	Name     string
	Homepage string
	Endpoint string
}

func runPup(html string, selector string) string {
	var buf bytes.Buffer
	cmd := exec.Command("pup", selector)
	cmd.Stdin = strings.NewReader(html)
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		log.Printf("runPup failed with %v", err)
		return ""
	}
	return strings.TrimSpace(buf.String())
}

// extractJournalInfo extracts name and URL from raw HTML. Be insane and shellout to use pup.
func extractJournalInfo(html string) (*JournalInfo, error) {
	// cat page-000281.html | pup 'h3 text{}' # Journal of Modern Materials
	// cat page-000281.html | pup 'p.archiveLinks > a:nth-child(2) attr{href}' # https://journals.aijr.in/index.php/jmm/index
	return &JournalInfo{
		Name:     runPup(html, "'h3 text{}'"),
		Homepage: runPup(html, "'p.archiveLinks > a:nth-child(2) attr{href}'"),
	}, nil
}

func main() {
	flag.Parse()
	// Create target directory.
	target := path.Join(*cacheDir, *tag)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		if err := os.MkdirAll(target, 0755); err != nil {
			log.Fatal(err)
		}
	}
	client := pester.New()
	client.SetRetryOnHTTP429(true)
	id := 0
	for i := 0; i < *maxID; i++ {
		// wrapFunc, so we can enjoy the defer on resp.Body.
		wrapFunc := func() {
			id++
			// https: //index.pkp.sfu.ca/index.php/browse/archiveInfo/5000
			link := fmt.Sprintf("%s/archiveInfo/%d", *baseURL, id)
			filename := fmt.Sprintf("page-%06d.html", id)
			dst := path.Join(target, filename)
			if fi, err := os.Stat(dst); err == nil {
				if fi.Size() > 0 || !*force {
					log.Printf("already cached %s %s", dst, link)
					return
				}
				if *verbose {
					log.Printf("force redownload: %s", link)
				}
			}
			resp, err := client.Get(link)
			if err != nil {
				log.Fatal(err)
			}
			if resp.StatusCode >= 400 {
				log.Fatal("failed with %s", resp.Status)
			}
			defer resp.Body.Close()
			// refresh: 0; url=https://index.pkp.sfu.ca/index.php/browse
			refresh := resp.Header.Get("refresh")
			if refresh != "" {
				log.Printf("[touch] refresh found for %s", link)
				// Just touch.
				if err := WriteFileAtomic(dst, []byte{}, 0644); err != nil {
					log.Fatal(err)
				}
				return
			}
			b, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Fatal(err)
			}
			if err := WriteFileAtomic(dst, b, 0644); err != nil {
				log.Fatal(err)
			}
			if *verbose {
				log.Printf("done: %s %s", dst, link)
			}
			time.Sleep(*sleep)
		}
		wrapFunc()
	}
}

// WriteFileAtomic writes the data to a temp file and atomically move if everything else succeeds.
func WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	dir, name := path.Split(filename)
	f, err := ioutil.TempFile(dir, name)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if permErr := os.Chmod(f.Name(), perm); err == nil {
		err = permErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filename)
	}
	// Any err should result in full cleanup.
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
