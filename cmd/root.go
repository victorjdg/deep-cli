package cmd

import (
	"fmt"
	"os"

	"github.com/victorjdg/deep-cli/internal/config"
	"github.com/victorjdg/deep-cli/internal/tui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version = "dev"

func SetVersion(v string) {
	version = v
}

var rootCmd = &cobra.Command{
	Use:   "deepseek",
	Short: "DeepSeek Coder CLI - AI programming assistant",
	Long:  "An interactive AI coding assistant powered by DeepSeek, with local Ollama and cloud API support.",
	Version: version,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		return tui.Run(cfg)
	},
}

func Execute() {
	rootCmd.Version = version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("api-key", "k", "", "DeepSeek API key")
	rootCmd.PersistentFlags().StringP("model", "m", "", "Model to use")
	rootCmd.PersistentFlags().BoolP("local", "l", false, "Force local Ollama mode")
	rootCmd.PersistentFlags().String("ollama-host", "", "Ollama host URL")
	rootCmd.PersistentFlags().Int("max-context", 0, "Maximum context window size in tokens (default: auto-detected by model)")

	_ = viper.BindPFlag("api-key", rootCmd.PersistentFlags().Lookup("api-key"))
	_ = viper.BindPFlag("model", rootCmd.PersistentFlags().Lookup("model"))
	_ = viper.BindPFlag("local", rootCmd.PersistentFlags().Lookup("local"))
	_ = viper.BindPFlag("ollama-host", rootCmd.PersistentFlags().Lookup("ollama-host"))
	_ = viper.BindPFlag("max-context", rootCmd.PersistentFlags().Lookup("max-context"))
}
