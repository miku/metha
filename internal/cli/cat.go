package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"

	"github.com/miku/metha"
	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
	"github.com/spf13/cobra"
)

func newCatCmd() *cobra.Command {
	var (
		format      string
		set         string
		baseDir     string
		from        string
		until       string
		root        string
		useJSON     bool
		setSpec     string
		deleted     bool
		onlyDeleted bool

		// Unbounded by default, where export bounds by default. The asymmetry is
		// the difference in what is being asked: export is a corpus dump over a
		// quarter of a million repositories, where one pathological record must
		// not take the run down, and cat is someone looking at one endpoint they
		// named - very possibly the pathological one, precisely because export
		// just reported it. A tool for inspecting a shard should not be the one
		// that hides what is in it.
		maxRecordBytes int
	)
	cmd := &cobra.Command{
		Use:     "cat ENDPOINT",
		Short:   "Write harvested records to stdout",
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"metha-cat"},
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := store.Open(baseDir, store.Identity{
				BaseURL: oai.PrependSchema(args[0]),
				Format:  format,
				Set:     set,
			})
			if err != nil {
				return err
			}
			bw := bufio.NewWriter(os.Stdout)
			// Deleted records are suppressed unless asked for: they carry no
			// metadata, and a consumer that wants the tombstones is asking a
			// different question than one reading records.
			policy := store.DeletedSkip
			switch {
			case onlyDeleted:
				policy = store.DeletedOnly
			case deleted:
				policy = store.DeletedKeep
			}
			err = store.Render(st, store.RenderOpts{
				Writer:         bw,
				From:           from,
				Until:          until,
				SetSpec:        setSpec,
				Deleted:        policy,
				Root:           root,
				UseJson:        useJSON,
				MaxRecordBytes: maxRecordBytes,
				// To stderr, so it does not land in the records on stdout.
				Oversize: func(id string, n int) {
					fmt.Fprintf(os.Stderr, "skipping %s: %s over --max-record-bytes\n",
						id, humanBytes(int64(n)))
				},
			})
			// Flushed here rather than in a defer, because the error matters.
			// Everything this command writes goes through the buffer, so the
			// last chunk of a corpus reaches the pipe in this call - and a
			// deferred flush drops what it says. A full disk or a reader that
			// went away then left "metha cat" exiting 0 on truncated output,
			// which is the failure a pipeline cannot see.
			return errors.Join(err, bw.Flush())
		},
	}
	f := cmd.Flags()
	f.StringVar(&format, "format", "oai_dc", "metadata format")
	f.StringVar(&set, "set", "", "set name")
	f.StringVar(&baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.StringVar(&from, "from", "", "ignore records before this date")
	f.StringVar(&until, "until", "", "ignore records after this date")
	f.StringVar(&root, "root", "Records", "root element to wrap records into")
	f.BoolVarP(&useJSON, "json", "j", false, "output json, not xml")
	f.StringVar(&setSpec, "setspec", "", "only records carrying this setSpec")
	f.BoolVar(&deleted, "deleted", false, "include records the endpoint marked deleted")
	f.BoolVar(&onlyDeleted, "only-deleted", false, "emit only the records the endpoint marked deleted")
	f.IntVar(&maxRecordBytes, "max-record-bytes", 0, "skip records whose metadata exceeds this many bytes; 0 for no bound")
	return cmd
}
