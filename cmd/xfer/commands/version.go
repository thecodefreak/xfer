package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("xfer version %s (commit %s, built %s)\n", Version, Commit, BuildDate)
		fmt.Println("Secure file transfer via QR codes")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
