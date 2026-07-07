package ai

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	fmt.Println("\033[1;36m  AI Log Analysis\033[0m")
	fmt.Println("\033[1;36m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")

	fmt.Println()
	fmt.Println("\033[1;31mRoot Cause:\033[0m")
	fmt.Printf("   %s\n", result.RootCause)

	if len(result.Suggestions) > 0 {
		fmt.Println()
		fmt.Println("\033[1;33mSuggestions:\033[0m")
		for i, s := range result.Suggestions {
			fmt.Printf("   %d. %s\n", i+1, s)
		}
	}

	if len(result.FixCommands) > 0 {
		fmt.Println()
		fmt.Println("\033[1;32mFix Commands:\033[0m")
		for _, cmd := range result.FixCommands {
			fmt.Printf("   \033[0;32m$ %s\033[0m\n", cmd)
		}
	}

	fmt.Println()
	fmt.Println("\033[1;36m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")
}

// gatherWorkspaceContext gathers files, git repo status, and dependency metadata for Gemini.
func gatherWorkspaceContext() string {
	cwd := os.Getenv("OPSAI_WORKSPACE_DIR")
	if cwd == "" {
		cwd, _ = os.Getwd()
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

// FileReference represents a matched error location in a log
type FileReference struct {
	Path string
	Line int
}

// ExtractErrorLocations parses logs for exact file and line number references
func ExtractErrorLocations(logContent string) []FileReference {
	var refs []FileReference
	seen := make(map[string]bool)

	// Regex patterns for different ecosystems
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`File "([^"]+)", line (\d+)`),                 // Python traceback
		regexp.MustCompile(`at .* \(([^:]+):(\d+):\d+\)`),                 // Node.js stack trace
		regexp.MustCompile(`([^:\s]+):(\d+):\s`),                          // Go panic / compiler error
		regexp.MustCompile(`at .*\(([^:]+\.java):(\d+)\)`),                // Java stack trace
		regexp.MustCompile(`(?:^|\s)([\w\.-/]+\.(?:py|js|ts|go|java|yaml|yml|tmpl|sh|xml)):(\d+)`), // Generic fallback
	}

	lines := strings.Split(logContent, "\n")
	for _, line := range lines {
		for _, p := range patterns {
			matches := p.FindStringSubmatch(line)
			if len(matches) >= 3 {
				path := matches[1]
				var lineNum int
				fmt.Sscanf(matches[2], "%d", &lineNum)

				// Deduplicate
				key := fmt.Sprintf("%s:%d", path, lineNum)
				if !seen[key] {
					refs = append(refs, FileReference{Path: path, Line: lineNum})
					seen[key] = true
				}
			}
		}
	}
	return refs
}

// normalizeContainerPath attempts to map container paths (e.g., /app/src) to host paths
func normalizeContainerPath(cwd, path string) string {
	path = strings.TrimPrefix(path, "/app/")
	path = strings.TrimPrefix(path, "/workspace/")
	return filepath.Join(cwd, path)
}

// ReadFileSnippets reads a window of lines around the error location
func ReadFileSnippets(cwd string, refs []FileReference) string {
	var sb strings.Builder
	for _, ref := range refs {
		localPath := normalizeContainerPath(cwd, ref.Path)

		file, err := os.Open(localPath)
		if err != nil {
			continue // Skip if we can't find the file locally
		}

		scanner := bufio.NewScanner(file)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		file.Close()

		// Calculate window (10 lines above and below)
		start := ref.Line - 10
		if start < 1 {
			start = 1
		}
		end := ref.Line + 10
		if end > len(lines) {
			end = len(lines)
		}

		sb.WriteString(fmt.Sprintf("\n--- %s (Lines %d-%d) ---\n", ref.Path, start, end))
		for i := start; i <= end; i++ {
			prefix := "  "
			if i == ref.Line {
				prefix = ">>" // Highlight the exact error line
			}
			sb.WriteString(fmt.Sprintf("%s %4d | %s\n", prefix, i, lines[i-1]))
		}
	}
	return sb.String()
}

func AnalyzeLogs(logContent string) (AnalysisResult, error) {
	var result AnalysisResult

	client, err := NewClient()
	if err != nil {
		return result, err
	}
	defer client.Close()

	cwd := os.Getenv("OPSAI_WORKSPACE_DIR")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

systemPrompt := fmt.Sprintf(`You are an expert Principal Site Reliability Engineer (SRE) and DevOps Architect.
Analyze the provided CI/CD pipeline/command failure logs within the context of the user's workspace and the SOURCE CODE AT ERROR LOCATIONS section if present.

ANTI-HALLUCINATION RULES — THESE OVERRIDE ALL OTHER INSTINCTS:
1. Only reference file paths, line numbers, or code shown in the CODE CONTEXT or LOG sections below. Never invent a file path that wasn't explicitly shown to you.
2. If you cannot identify the responsible file, say so explicitly instead of guessing.
3. Quote exact error strings, exception class names, and exit codes from the log verbatim.

Follow these strict rules for 'fix_commands':
1. The user's host OS is '%s'. You MUST tailor all shell commands to this OS.
   - If 'windows', NEVER use 'sed', 'awk', or 'grep'. You MUST use native PowerShell commands. For text replacement, use: (Get-Content path) -replace 'old', 'new' | Set-Content path
   - If 'darwin' (macOS), use macOS-specific sed syntax: 'sed -i "" ...'
   - If 'linux', use standard GNU sed.
2. Check if 'Is Git Repository' is true. If false, DO NOT suggest git commands like 'git push' or 'git commit'.
3. Keep commands concise, practical, and safe to execute.
4. Never suggest destructive commands (rm -rf, sudo rm, DROP TABLE) unless the log explicitly shows corruption requiring it.
5. If a command requires interactive manual input (e.g., entering passwords), explicitly document this in the 'suggestions' field.

Assess the severity of the failure:
- If the failure is a non-blocking linter warning or style issue that does not break compilation or testing, set 'severity' to 'warning' and 'ignorable' to true.
- If it breaks compilation, testing, or deployment, set 'severity' to 'critical' and 'ignorable' to false.

Respond with ONLY valid JSON in this exact structure:
{
  "root_cause": "string",
  "confidence": "high | medium | low",
  "suggestions": ["string"],
  "fix_commands": ["string"],
  "severity": "critical | warning | info",
  "ignorable": true
}`, runtime.GOOS)

	workspaceContext := gatherWorkspaceContext()

	truncated := logContent
	if len(truncated) > 12000 {
		truncated = "...[truncated]\n" + truncated[len(truncated)-12000:]
	}

	// Extract real file:line references and read the actual code snippets
	refs := ExtractErrorLocations(logContent)
	codeContext := ""
	if len(refs) > 0 {
		codeContext = "\n=== SOURCE CODE AT ERROR LOCATIONS ===\n" + ReadFileSnippets(cwd, refs)
	}

	userMessage := fmt.Sprintf("Workspace Context:\n%s%s\n\nFailed Pipeline Logs:\n%s",
		workspaceContext, codeContext, truncated)

	responseText, err := client.Complete(systemPrompt, userMessage)
	if err != nil {
		return result, fmt.Errorf("log analysis failed: %w", err)
	}

	// JSON parsing
	cleaned := strings.TrimSpace(responseText)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	type rawResult struct {
		RootCause   string   `json:"root_cause"`
		Confidence  string   `json:"confidence"`
		Suggestions []string `json:"suggestions"`
		FixCommands []string `json:"fix_commands"`
	}
	var raw rawResult

	if err := parseJSON(cleaned, &raw); err != nil {
		return result, fmt.Errorf("AI returned invalid JSON: %w", err)
	}

	// Format confidence into the root cause for CLI display
	result.RootCause = fmt.Sprintf("[%s confidence] %s", strings.ToUpper(raw.Confidence), raw.RootCause)
	result.Suggestions = raw.Suggestions
	result.FixCommands = raw.FixCommands

	return result, nil
}
