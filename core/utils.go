package core

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"devsandbox/core/ai"
)

// AnalyzeProject returns a map indicating project characteristics
func AnalyzeProject() map[string]bool {
	cwd, _ := os.Getwd()

	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(cwd, name))
		return err == nil
	}

	return map[string]bool{
		"has_docker": exists("Dockerfile"),
		"has_k8s":    exists("k8s") || exists("kubernetes") || exists("kind"),
	}
}

// ExecSilent executes a native OS command silently and returns the error
func ExecSilent(executable string, args ...string) error {
	cmd := exec.Command(executable, args...)
	return cmd.Run()
}

// ExecCommand executes a native OS command without needing a bash wrapper
func ExecCommand(stepName string, ignoreErrors bool, liveOutput bool, executable string, args ...string) {
	fmt.Printf("\n\033[1;36m▶ Running: %s...\033[0m\n", stepName)

	cmd := exec.Command(executable, args...)
	var outBuf bytes.Buffer

	if liveOutput {
		// THE SPLITTER: Streams to terminal AND captures in memory for the AI
		multiWriter := io.MultiWriter(os.Stdout, &outBuf)
		cmd.Stdout = multiWriter
		cmd.Stderr = multiWriter
	} else {
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf
	}

	err := cmd.Run()
	outputStr := outBuf.String()

	if err != nil {
		handleError(err, stepName, ignoreErrors, outputStr)
		return
	}

	fmt.Printf("\033[32m✅ %s completed perfectly!\033[0m\n", stepName)
}

func handleError(err error, stepName string, ignoreErrors bool, output string) {
	if !ignoreErrors {
		fmt.Printf("\033[1;31m❌ Error during %s.\033[0m\n", stepName)

		// Auto AI analysis -- only if API key is present
		if os.Getenv("GEMINI_API_KEY") != "" && (output != "" || err != nil) {
			triggerAutoAnalysis(stepName, output, err)
		} else if output != "" {
			fmt.Print(output)
		}
		os.Exit(1)
	} else {
		fmt.Printf("\033[33m⚠️ %s found issues (Ignored):\033[0m\n", stepName)
		if output != "" {
			fmt.Print(output)
		}
	}
}

func triggerAutoAnalysis(stepName string, output string, originalErr error) {
	fmt.Println("\n\033[1;35m🤖 Terminal crashed. Auto-analyzing failure with Gemini AI...\033[0m")

	contextStr := fmt.Sprintf("Step: %s\n", stepName)
	if originalErr != nil {
		contextStr += fmt.Sprintf("Error: %s\n", originalErr.Error())
	}
	if output != "" {
		contextStr += fmt.Sprintf("Output:\n%s", output)
	}

	analysis, err := ai.AnalyzeLogs(contextStr)
	if err != nil {
		fmt.Printf("\033[1;31m❌ AI log analysis failed: %v\033[0m\n", err)
		return
	}
	ai.PrintAnalysis(analysis)

	AskAndApplyFixes(analysis)
}

// AskAndApplyFixes prompts the user to apply the fix commands suggested by the AI.
func AskAndApplyFixes(result ai.AnalysisResult) {
	if len(result.FixCommands) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("\033[1;32m🔧 Actionable Fixes Found:\033[0m")

	var shell, flag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		flag = "/C"
	} else {
		shell = "bash"
		flag = "-c"
	}

	reader := bufio.NewReader(os.Stdin)

	for i, fixCmdStr := range result.FixCommands {
		trimmedCmd := strings.TrimSpace(fixCmdStr)
		if trimmedCmd == "" {
			continue
		}

		fmt.Printf("\n\033[1;33m[%d/%d] Suggested Command:\033[0m \033[1;37m%s\033[0m\n", i+1, len(result.FixCommands), trimmedCmd)
		fmt.Print("👉 Do you want to run this command? (y/N): ")

		response, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("\033[1;31m❌ Error reading input: %v\033[0m\n", err)
			os.Exit(1)
		}
		response = strings.TrimSpace(strings.ToLower(response))

		if response == "y" || response == "yes" {
			fmt.Printf("\033[1;36m▶ Running command...\033[0m\n")
			execCmd := exec.Command(shell, flag, trimmedCmd)
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr
			execCmd.Stdin = os.Stdin

			if err := execCmd.Run(); err != nil {
				fmt.Printf("\033[1;31m❌ Command failed: %v\033[0m\n", err)
				os.Exit(1)
			}
			fmt.Println("\033[1;32m✓ Command completed successfully.\033[0m")
		} else {
			fmt.Println("\033[90mSkipped.\033[0m")
		}
	}
}

// RequireProjectRoot ensures the user is executing the command from
// the root of the repository by looking for pipeline.yaml.
func RequireProjectRoot(cwd string) {
	yamlPath := filepath.Join(cwd, "pipeline.yaml")
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		fmt.Println("\033[1;31m❌ Error: Project configuration not found.\033[0m")
		fmt.Println("\033[33m💡 Suggestion: Please ensure you are running this command from the root directory of your project (where pipeline.yaml is located).\033[0m")
		os.Exit(1)
	}
}

// --- HELPERS FOR SANDBOX 2.GO ---

var trackedTempFiles []string

// TrackTempFile registers a temporary file so it can be cleaned up globally
// if the user hits Ctrl+C before the defer statements trigger.
func TrackTempFile(path string) {
	trackedTempFiles = append(trackedTempFiles, path)
}

// PollEndpoint pings a URL repeatedly until it returns an HTTP 200 or the context times out.
func PollEndpoint(ctx context.Context, url string, timeout time.Duration, interval time.Duration) error {
	client := &http.Client{Timeout: 3 * time.Second}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout exceeded waiting for %s", url)
		case <-ticker.C:
			resp, err := client.Get(url)
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}
}
