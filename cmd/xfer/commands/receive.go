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
	"github.com/thecodefreak/xfer/internal/client"
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
		if err := cfg.Validate(); err != nil {
			return err
		}

		outputDir := cfg.OutputDir
		if len(args) > 0 {
			outputDir = args[0]
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

		opts := client.ReceiveOptions{
			OutputDir:    outputDir,
			ServerURL:    cfg.Server,
			Insecure:     cfg.Insecure,
			Timeout:      cfg.Timeout,
			ShowProgress: cfg.Progress,
		}

		if err := client.Receive(ctx, opts); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
			if isReceiveError(err) {
				fmt.Fprintln(os.Stderr, "\nThe transfer failed. Please try again:")
				fmt.Fprintln(os.Stderr, "  1. Ask the sender to re-upload the file")
				fmt.Fprintln(os.Stderr, "  2. Run 'xfer receive' again to get a new upload link")
			}
			return nil
		}
		return nil
	},
}

func isReceiveError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "incomplete") ||
		strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "transfer") ||
		strings.Contains(errStr, "timeout")
}

func init() {
	rootCmd.AddCommand(receiveCmd)
}
