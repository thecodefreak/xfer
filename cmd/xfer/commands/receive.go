package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var receiveCmd = &cobra.Command{
	Use:   "receive [output-dir]",
	Short: "Receive files from another device",
	Long: `Receive files from another device via QR code.

The sender scans the QR code with their mobile device to upload files.

Examples:
  xfer receive                    # Receive to current directory
  xfer receive ./downloads        # Receive to specific directory
  xfer receive ~/Documents`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Receive command - not yet implemented")
		os.Exit(0)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(receiveCmd)
}
