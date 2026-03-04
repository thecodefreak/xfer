package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("xfer version 0.1.0")
		fmt.Println("Secure file transfer via QR codes")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
