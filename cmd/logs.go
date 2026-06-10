package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devsandbox/core/ai"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "AI-powered log analysis tools",
}

var logsAnalyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze pipeline or Kubernetes logs and get AI-powered fix suggestions",
	Long:  `Reads log content from stdin or a file and uses AI to diagnose failures and suggest fixes.`,
	Run: func(cmd *cobra.Command, args []string) {
		logFile, _ := cmd.Flags().GetString("file")

		var logContent []byte
		var err error

		if logFile != "" {
			logContent, err = os.ReadFile(logFile)
			if err != nil {
				fmt.Printf("\033[1;31m❌ Could not read file: %v\033[0m\n", err)
				os.Exit(1)
			}
		} else {
			stat, err := os.Stdin.Stat()
			if err == nil && (stat.Mode()&os.ModeCharDevice) != 0 {
				// We are in an interactive terminal. Attempt to auto-fetch from Jenkins.
				cwd, err := os.Getwd()
				if err == nil {
					rawName := filepath.Base(cwd)
					appName := strings.ToLower(rawName)
					appName = strings.ReplaceAll(appName, "_", "-")
					appName = strings.ReplaceAll(appName, " ", "-")

					fmt.Printf("\033[1;36m🔎 Attempting to auto-fetch latest logs from local Jenkins for '%s'...\033[0m\n", appName)
					url := fmt.Sprintf("%s/job/%s/lastBuild/consoleText", loadSandboxPorts().JenkinsURL(), appName)
					req, err := http.NewRequest("GET", url, nil)
					if err == nil {
						req.SetBasicAuth("admin", "admin")
						client := &http.Client{Timeout: 5 * time.Second}
						resp, err := client.Do(req)
						if err == nil && resp.StatusCode == 200 {
							defer resp.Body.Close()
							body, err := io.ReadAll(resp.Body)
							if err == nil && len(body) > 0 {
								logContent = body
								fmt.Printf("\033[1;32m✓ Successfully fetched latest logs for job '%s'\033[0m\n", appName)
							}
						}
					}
				}

				// If auto-fetch failed or wasn't possible, fall back to manual copy-paste
				if len(logContent) == 0 {
					fmt.Println("\033[33m⚠️  No piped input and could not auto-fetch logs from local Jenkins.\033[0m")
					fmt.Println("\033[33mPaste your log output below, then press Ctrl+D when done:\033[0m")
					logContent, err = io.ReadAll(os.Stdin)
					if err != nil {
						fmt.Printf("\033[1;31m❌ Could not read stdin: %v\033[0m\n", err)
						os.Exit(1)
					}
				}
			} else {
				// Piped stdin stream
				logContent, err = io.ReadAll(os.Stdin)
				if err != nil {
					fmt.Printf("\033[1;31m❌ Could not read stdin: %v\033[0m\n", err)
					os.Exit(1)
				}
			}
		}

		if len(logContent) == 0 {
			fmt.Println("\033[1;31m❌ No log content provided.\033[0m")
			os.Exit(1)
		}

		// Print highlighted summary of warnings/errors
		printHighlightedLogSummary(string(logContent))

		fmt.Println("\033[1;36m🤖 Analyzing logs...\033[0m")

		result, err := ai.AnalyzeLogs(string(logContent))
		if err != nil {
			fmt.Printf("\033[1;31m❌ %s\033[0m\n", err.Error())
			os.Exit(1)
		}

		ai.PrintAnalysis(result)
	},
}

func init() {
	logsAnalyzeCmd.Flags().StringP("file", "f", "", "Path to a log file (reads from stdin if not provided)")
	logsCmd.AddCommand(logsAnalyzeCmd)
	rootCmd.AddCommand(logsCmd)
}

func printHighlightedLogSummary(logContent string) {
	lines := strings.Split(logContent, "\n")
	printedCount := 0
	hasLogSummaries := false

	for i, line := range lines {
		lowerLine := strings.ToLower(line)
		isError := strings.Contains(lowerLine, "error") || strings.Contains(lowerLine, "failed") || strings.Contains(lowerLine, "exception")
		isWarning := strings.Contains(lowerLine, "warning") || strings.Contains(lowerLine, "warn")

		if isError || isWarning {
			if !hasLogSummaries {
				fmt.Println()
				fmt.Println("\033[1;35m📋 Detected Log Warnings/Errors:\033[0m")
				hasLogSummaries = true
			}
			if printedCount >= 10 {
				fmt.Println("\033[90m... and more warnings/errors found in the logs\033[0m")
				break
			}
			lineNumber := i + 1
			trimmedLine := strings.TrimSpace(line)
			if isError {
				fmt.Printf("\033[1;31m[ERROR]\033[0m %s (Log Line %d)\n", trimmedLine, lineNumber)
			} else {
				fmt.Printf("\033[1;33m[WARN]\033[0m %s (Log Line %d)\n", trimmedLine, lineNumber)
			}
			printedCount++
		}
	}
	if hasLogSummaries {
		fmt.Println()
	}
}
