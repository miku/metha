package cli

import (
	"github.com/miku/metha"
	"github.com/spf13/cobra"
)

// rootName is what the one binary is called, and what the deprecation notice
// points people at.
const rootName = "metha"

// NewRoot builds the command tree.
//
// Every verb keeps the flags its own executable had, single letters included:
// those stay pflag shorthands, so "-q" and "-T 30s" go on meaning what they
// meant, and only the multi letter flags need the second dash pflag insists on,
// which is what RewriteArgs supplies.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   rootName,
		Short: "No frills incremental OAI-PMH harvesting",
		Long: `metha harvests OAI-PMH endpoints incrementally and keeps the results in a
local cache, so a second run only fetches what is new.

Until 0.5 metha installed one executable per verb. They are still there, as
symlinks to this binary, and still take the flags they always did; "metha shim
install" lays them down again after a "go install".`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.AddCommand(
		newSyncCmd(),
		newCatCmd(),
		newLsCmd(),
		newFilesCmd(),
		newIDCmd(),
		newStatCmd(),
		newMigrateCmd(),
		newFortuneCmd(),
		newShimCmd(),
	)
	// -v is the version flag, as it was in every released command that had one.
	// Cobra adds it per command, and only takes the shorthand when it is free,
	// so a command that wants -v for something else can still claim it first.
	//
	// The flag has to belong to each command rather than be inherited from the
	// root, because cobra answers it before it validates arguments: a
	// persistent flag would leave "metha cat -v" complaining about a missing
	// endpoint instead of printing the version, which is what it printed for
	// every one of the last four years.
	setVersion(root, metha.Version)
	root.SetVersionTemplate("{{.Version}}\n")
	return root
}

// setVersion marks the whole tree with the version, which is what makes cobra
// give each command a --version flag of its own.
func setVersion(cmd *cobra.Command, version string) {
	cmd.Version = version
	for _, sub := range cmd.Commands() {
		setVersion(sub, version)
	}
}
