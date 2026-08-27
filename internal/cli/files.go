package cli

import (
	"fmt"

	"github.com/miku/metha"
	"github.com/miku/metha/store"
	"github.com/spf13/cobra"
)

func newFilesCmd() *cobra.Command {
	var format, set, baseDir string
	cmd := &cobra.Command{
		Use:     "files ENDPOINT",
		Short:   "List the files holding an endpoint's harvested data",
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"metha-files"},
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := store.Open(baseDir, store.Identity{
				BaseURL: metha.PrependSchema(args[0]),
				Format:  format,
				Set:     set,
			})
			if err != nil {
				return err
			}
			files, err := st.Files()
			if err != nil {
				return err
			}
			for _, fn := range files {
				fmt.Println(fn)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.StringVar(&format, "format", "oai_dc", "metadata format")
	f.StringVar(&set, "set", "", "set name")
	return cmd
}
