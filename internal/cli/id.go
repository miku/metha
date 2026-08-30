package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/miku/metha/oai"
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
				baseURL = oai.PrependSchema(args[0])
				repo    = oai.Repository{BaseURL: baseURL}
				m       = make(map[string]interface{})
				req     = oai.Request{Verb: "Identify", BaseURL: baseURL}
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
			resp, err := oai.StdClient.Do(&req)
			if err != nil {
				return err
			}
			m["identify"] = resp.Identify
			// Reported like the formats and the sets below, rather than ending
			// the command. The size is one optional attribute of one optional
			// element, and an endpoint that declines to give it has still told
			// us everything else this command asks for - failing the whole
			// report over it threw away an Identify that had already arrived.
			if size, err := repo.CompleteListSize(); err == nil {
				m["size"] = size
			} else {
				log.Println(err)
			}
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
