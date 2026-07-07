package cmd

import (
	"fmt"
	"os"
	"strings"

	"devsandbox/core"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "devsandbox",
	Short: "DevSandbox — Your AI-powered local CI/CD CLI",
	Long:  `DevSandbox is a local CI/CD pipeline and observability CLI tool powered by AI.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// The serve subcommand boots the web server from any directory;
		// it doesn't need to be in a project root and temp workspace paths
		// created by the server are always space-free.
		if cmd.Name() == "serve" {
			return
		}
		cwd := core.GetWorkspaceDir()
		if strings.Contains(cwd, " ") {
			fmt.Println("\033[1;31m\u274c Project path contains spaces, which are unsupported by the pipeline engine.\033[0m")
			fmt.Printf("\U0001f449 Current path: %s\n", cwd)
			fmt.Println("Please move or clone your project to a path without spaces and try again.")
			os.Exit(1)
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
