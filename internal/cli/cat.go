package cli

import (
	"bufio"
	"os"

	"github.com/miku/metha"
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
	)
	cmd := &cobra.Command{
		Use:     "cat ENDPOINT",
		Short:   "Write harvested records to stdout",
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"metha-cat"},
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := store.Open(baseDir, store.Identity{
				BaseURL: metha.PrependSchema(args[0]),
				Format:  format,
				Set:     set,
			})
			if err != nil {
				return err
			}
			bw := bufio.NewWriter(os.Stdout)
			defer bw.Flush()
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
			return store.Render(st, store.RenderOpts{
				Writer:  bw,
				From:    from,
				Until:   until,
				SetSpec: setSpec,
				Deleted: policy,
				Root:    root,
				UseJson: useJSON,
			})
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
	return cmd
}
