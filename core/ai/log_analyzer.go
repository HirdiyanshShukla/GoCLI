package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AnalysisResult holds the structured diagnosis returned by the AI log analyzer.
type AnalysisResult struct {
	RootCause   string
	Suggestions []string
	FixCommands []string
}

// PrintAnalysis prints a formatted AI diagnosis to stdout.
func PrintAnalysis(result AnalysisResult) {
	fmt.Println()
	fmt.Println("\033[1;36m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")
	fmt.Println("\033[1;36m  🤖 AI Log Analysis\033[0m")
	fmt.Println("\033[1;36m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")

	fmt.Println()
	fmt.Println("\033[1;31m🔍 Root Cause:\033[0m")
	fmt.Printf("   %s\n", result.RootCause)

	if len(result.Suggestions) > 0 {
		fmt.Println()
		fmt.Println("\033[1;33m💡 Suggestions:\033[0m")
		for i, s := range result.Suggestions {
			fmt.Printf("   %d. %s\n", i+1, s)
		}
	}

	if len(result.FixCommands) > 0 {
		fmt.Println()
		fmt.Println("\033[1;32m🔧 Fix Commands:\033[0m")
		for _, cmd := range result.FixCommands {
			fmt.Printf("   \033[0;32m$ %s\033[0m\n", cmd)
		}
	}

	fmt.Println()
	fmt.Println("\033[1;36m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")
}

// gatherWorkspaceContext gathers files, git repo status, and dependency metadata for Gemini.
func gatherWorkspaceContext() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "Unknown workspace directory"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Current Workspace Directory: %s\n", cwd))
	builder.WriteString(fmt.Sprintf("Host OS: %s\n", runtime.GOOS))

	// Check if git is initialized
	_, gitErr := os.Stat(filepath.Join(cwd, ".git"))
	isGit := gitErr == nil
	builder.WriteString(fmt.Sprintf("Is Git Repository: %t\n", isGit))

	// List root files
	files, err := os.ReadDir(cwd)
	if err == nil {
		builder.WriteString("Workspace Files:\n")
		for _, f := range files {
			if f.IsDir() {
				builder.WriteString(fmt.Sprintf("  - %s/\n", f.Name()))
			} else {
				builder.WriteString(fmt.Sprintf("  - %s\n", f.Name()))
			}
		}
	}

	// Read relevant dependency files if they exist
	depFiles := []string{"requirements.txt", "package.json", "go.mod", "Dockerfile", "Jenkinsfile"}
	for _, f := range depFiles {
		path := filepath.Join(cwd, f)
		if data, err := os.ReadFile(path); err == nil {
			content := string(data)
			if len(content) > 1000 {
				content = content[:1000] + "\n...[truncated]"
			}
			builder.WriteString(fmt.Sprintf("\nContent of %s:\n%s\n", f, content))
		}
	}

	return builder.String()
}

// AnalyzeLogs sends Jenkins or Kubernetes log output to Gemini and returns
// a structured diagnosis with actionable fix suggestions.
func AnalyzeLogs(logContent string) (AnalysisResult, error) {
	var result AnalysisResult

	client, err := NewClient()
	if err != nil {
		return result, err
	}
	defer client.Close()

	systemPrompt := `You are an expert Principal Site Reliability Engineer (SRE) and DevOps Architect.
Analyze the provided CI/CD pipeline/command failure logs within the context of the user's workspace.
Identify the precise root cause and provide specific, actionable, and platform-appropriate fixes.

Follow these strict rules for 'fix_commands':
1. Tailor the shell commands to the host OS. If host OS is 'darwin' (macOS) and you suggest using 'sed -i', use the macOS-specific syntax: 'sed -i \'\'' ...'.
2. Check if 'Is Git Repository' is true. If false, DO NOT suggest git commands like 'git push' or 'git commit'.
3. For python requirements.txt changes, suggest simple text replacement or commands appropriate to modify the file directly, avoiding complex regex if possible.
4. Keep commands concise, practical, and safe to execute.

Respond with ONLY valid JSON in this exact structure:
{
  "root_cause": "string — precise explanation of the root cause",
  "suggestions": ["string", "string"],
  "fix_commands": ["string — exact command to run", "string"]
}`

	workspaceContext := gatherWorkspaceContext()

	truncated := logContent
	if len(truncated) > 12000 {
		truncated = "...[truncated]\n" + truncated[len(truncated)-12000:]
	}

	userMessage := fmt.Sprintf("Workspace Context:\n%s\n\nFailed Pipeline Logs:\n%s", workspaceContext, truncated)

	responseText, err := client.Complete(systemPrompt, userMessage)
	if err != nil {
		return result, fmt.Errorf("log analysis failed: %w", err)
	}

	cleaned := strings.TrimSpace(responseText)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	type rawResult struct {
		RootCause   string   `json:"root_cause"`
		Suggestions []string `json:"suggestions"`
		FixCommands []string `json:"fix_commands"`
	}
	var raw rawResult

	if err := parseJSON(cleaned, &raw); err != nil {
		return result, fmt.Errorf("AI returned invalid JSON: %w", err)
	}

	result.RootCause = raw.RootCause
	result.Suggestions = raw.Suggestions
	result.FixCommands = raw.FixCommands

	return result, nil
}
