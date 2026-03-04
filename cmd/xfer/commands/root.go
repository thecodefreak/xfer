package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"xfer/internal/config"
)

var (
	cfgServer   string
	cfgInsecure bool
	cfgTimeout  string
	cfgProgress bool

	cfg *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "xfer",
	Short: "Secure file transfer via QR codes",
	Long: `xfer is a secure, end-to-end encrypted file transfer tool that uses 
QR codes to transfer files between devices over the internet.

Use 'xfer send' to send files and 'xfer receive' to receive files.
The recipient scans the QR code with their mobile device to complete the transfer.

All transfers are encrypted end-to-end - the server never sees your files.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "config" || cmd.Name() == "version" || cmd.Name() == "setup" || cmd.Name() == "info" || cmd.Name() == "test" {
			return nil
		}

		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if cfgServer != "" {
			cfg.Server = cfgServer
		}
		if cmd.Flags().Changed("insecure") {
			cfg.Insecure = cfgInsecure
		}
		if cfgTimeout != "" {
			if err := cfg.Set("timeout", cfgTimeout); err != nil {
				return err
			}
		}
		if cmd.Flags().Changed("progress") {
			cfg.Progress = cfgProgress
		}

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgServer, "server", "s", "", "Server URL")
	rootCmd.PersistentFlags().BoolVarP(&cfgInsecure, "insecure", "k", false, "Skip TLS certificate verification")
	rootCmd.PersistentFlags().StringVar(&cfgTimeout, "timeout", "", "Transfer timeout (e.g., 10m)")
	rootCmd.PersistentFlags().BoolVar(&cfgProgress, "progress", true, "Show detailed progress")
}

func Execute() error {
	return rootCmd.Execute()
}
