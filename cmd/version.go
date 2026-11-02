package cmd

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Set at build time by GoReleaser via -ldflags.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the snoo-dl version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "snoo-dl %s", Version())
		if commit != "" {
			fmt.Fprintf(out, " (%s", commit)
			if date != "" {
				fmt.Fprintf(out, ", %s", date)
			}
			fmt.Fprint(out, ")")
		}
		fmt.Fprintf(out, " %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.Version = Version()
}

// Version returns the release version, or the module version when installed
// with `go install`, or "dev".
func Version() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}
