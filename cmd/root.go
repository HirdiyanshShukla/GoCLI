package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "devsandbox",
	Short: "DevSandbox — Your AI-powered local CI/CD CLI",
	Long:  `DevSandbox is a local CI/CD pipeline and observability CLI tool powered by AI.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
