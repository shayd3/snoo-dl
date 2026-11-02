package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "snoo-dl",
	Short: "Snoo-dl grabs images from a subreddit and downloads them!",
	Long: `Snoo-dl is a CLI that grabs images from one or more subreddits and
downloads them to a local folder.

Every download flag can also be set in the config file
($HOME/.snoodl.yaml by default) or with an environment variable
prefixed with SNOODL_, e.g. SNOODL_LOCATION=~/Pictures.`,
	SilenceUsage: true,
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		return initConfig()
	},
}

// Execute runs the root command. Ctrl-C cancels in-flight requests, and the
// process exits non-zero on any error.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := rootCmd.ExecuteContext(ctx)
	stop()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "Interrupted.")
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.snoodl.yaml)")
}

// initConfig reads the config file and SNOODL_* environment variables.
func initConfig() error {
	viper.SetEnvPrefix("SNOODL")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
		if err := viper.ReadInConfig(); err != nil {
			return fmt.Errorf("reading config file %s: %w", cfgFile, err)
		}
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// No home directory: run with flags and env vars only.
		return nil
	}
	viper.AddConfigPath(home)
	viper.SetConfigType("yaml")
	viper.SetConfigName(".snoodl")

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("reading config file: %w", err)
	}
	return nil
}
