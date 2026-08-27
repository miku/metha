package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newIDCmd() *cobra.Command {
	var showSizeOnly bool
	cmd := &cobra.Command{
		Use:     "id ENDPOINT",
		Short:   "Ask an endpoint to identify itself",
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"metha-id"},
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				baseURL = metha.PrependSchema(args[0])
				repo    = metha.Repository{BaseURL: baseURL}
				m       = make(map[string]interface{})
				req     = metha.Request{Verb: "Identify", BaseURL: baseURL}
			)
			if showSizeOnly {
				size, err := repo.CompleteListSize()
				if err != nil {
					log.Println(err)
					return nil
				}
				fmt.Printf("%s\t%d\n", baseURL, size)
				return nil
			}
			resp, err := metha.StdClient.Do(&req)
			if err != nil {
				return err
			}
			m["identify"] = resp.Identify
			size, err := repo.CompleteListSize()
			if err != nil {
				return err
			}
			m["size"] = size
			if formats, err := repo.Formats(); err == nil {
				m["formats"] = formats
			} else {
				log.Println(err)
			}
			if sets, err := repo.Sets(); err == nil {
				m["sets"] = sets
			} else {
				log.Println(err)
			}
			if err := json.NewEncoder(os.Stdout).Encode(m); err != nil {
				return err
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().BoolVarP(&showSizeOnly, "size", "s", false, "show size only")
	return cmd
}
