package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/miku/metha"
	"github.com/miku/metha/store"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newLsCmd() *cobra.Command {
	var (
		baseDir             string
		showAll, bestEffort bool
	)
	cmd := &cobra.Command{
		Use:     "ls",
		Short:   "List the endpoints in the cache",
		Args:    cobra.NoArgs,
		Aliases: []string{"metha-ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			for entry, err := range store.List(baseDir) {
				if err != nil {
					if bestEffort {
						log.Println(err)
						continue
					}
					return err
				}
				name := filepath.Base(entry.Dir)
				if !showAll {
					name = ellipsis(name, 35)
				}
				id := entry.Identity
				fmt.Printf("%s\t%s\t%s\t%s\n", name, id.Set, id.Format, id.BaseURL)
			}
			legacyFooter(os.Stderr, baseDir)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.BoolVarP(&showAll, "all", "a", false, "show full path")
	f.BoolVarP(&bestEffort, "best-effort", "b", false, "continue in the presence of errors")
	return cmd
}

func ellipsis(s string, length int) string {
	if len(s) > length {
		return s[:length] + "..."
	}
	return s
}
