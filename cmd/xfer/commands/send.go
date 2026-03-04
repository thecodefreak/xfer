package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"xfer/internal/client"
)

var sendZip bool
var sendPassword string

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
		if err := cfg.Validate(); err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigChan
			fmt.Println("\nCancelling transfer...")
			cancel()
		}()

		opts := client.SendOptions{
			Files:        args,
			ServerURL:    cfg.Server,
			Insecure:     cfg.Insecure,
			Timeout:      cfg.Timeout,
			ShowProgress: cfg.Progress,
			Password:     sendPassword,
		}

		if err := client.Send(ctx, opts); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
			if isTransferError(err) {
				fmt.Fprintln(os.Stderr, "\nThe transfer failed. Please try again:")
				fmt.Fprintln(os.Stderr, "  1. Run 'xfer send <file>' again")
				fmt.Fprintln(os.Stderr, "  2. Have the receiver scan the new QR code")
			}
			return nil
		}
		return nil
	},
}

func isTransferError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "transfer") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "session")
}

func init() {
	sendCmd.Flags().BoolVarP(&sendZip, "zip", "z", false, "Zip files before sending")
	sendCmd.Flags().StringVar(&sendPassword, "password", "", "Password protect the transfer")
	rootCmd.AddCommand(sendCmd)
}
