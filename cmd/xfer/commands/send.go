package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var sendCmd = &cobra.Command{
	Use:   "send <file> [files...]",
	Short: "Send files to another device",
	Long: `Send one or more files to another device via QR code.

The recipient scans the QR code with their mobile device to download the files.

Folders and multiple files are automatically zipped into a single download.

Examples:
  xfer send myfile.txt
  xfer send file1.txt file2.txt   # Auto-zipped
  xfer send folder/               # Auto-zipped
  xfer send -z myfile.txt         # Force zip a single file
  xfer send --password myfile.txt # Password protected`,
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Send command - not yet implemented")
		os.Exit(0)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(sendCmd)
}
