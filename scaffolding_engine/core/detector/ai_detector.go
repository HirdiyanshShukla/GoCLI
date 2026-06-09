package detector

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"devsandbox/core/ai"
	"devsandbox/scaffolding_engine/core/rules"
)

// AIDetectionResult holds all variables returned by AI framework detection.
type AIDetectionResult struct {
	Framework   string `json:"framework"`
	Runtime     string `json:"runtime"`
	EntryPath   string `json:"entry_path"`
	AppPort     int    `json:"app_port"`
	HealthPath  string `json:"health_path"`
	TestCommand string `json:"test_command"`
	TestImage   string `json:"test_image"`
	RunCommand  string `json:"run_command"`
}

// buildProjectSnapshot reads the project directory tree and key file contents
// to build a text snapshot for the AI to analyse.
func buildProjectSnapshot(projectPath string) (string, error) {
	var sb strings.Builder

	sb.WriteString("=== PROJECT FILE TREE ===\n")

	err := filepath.WalkDir(projectPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && ignoreDirs[d.Name()] {
			return filepath.SkipDir
		}
		relPath, _ := filepath.Rel(projectPath, path)
		if d.IsDir() {
			sb.WriteString(relPath + "/\n")
		} else {
			sb.WriteString(relPath + "\n")
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	// Key signal files whose content provides strong framework hints
	signalFiles := []string{
		"go.mod", "Cargo.toml", "Gemfile", "*.csproj",
		"package.json", "requirements.txt", "pom.xml",
		"build.gradle", "Dockerfile", "docker-compose.yml",
		"main.go", "main.rb", "main.rs", "Program.cs",
	}

	sb.WriteString("\n=== KEY FILE CONTENTS ===\n")

	for _, pattern := range signalFiles {
		matches, _ := filepath.Glob(filepath.Join(projectPath, pattern))
		for _, match := range matches {
			data, err := os.ReadFile(match)
			if err != nil {
				continue
			}
			relPath, _ := filepath.Rel(projectPath, match)
			content := string(data)
			if len(content) > 2000 {
				content = content[:2000] + "\n...[truncated]"
			}
			sb.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", relPath, content))
		}
	}

	return sb.String(), nil
}

// AIDetectFramework uses Gemini to identify the framework and
// return all variables needed for scaffolding generation.
func AIDetectFramework(projectPath string) (AIDetectionResult, error) {
	var result AIDetectionResult

	client, err := ai.NewClient()
	if err != nil {
		return result, err
	}
	defer client.Close()

	snapshot, err := buildProjectSnapshot(projectPath)
	if err != nil {
		return result, fmt.Errorf("failed to read project: %w", err)
	}

	rulesData, err := rules.Files.ReadFile("rules.yaml")
	if err != nil {
		return result, fmt.Errorf("failed to read rules.yaml: %w", err)
	}

	pkgManager := DetectPackageManager(projectPath)

	systemPrompt := fmt.Sprintf(`You are a platform engineering assistant for a DevOps CLI tool.
Your job is to analyze a project's file structure and key file contents, then identify the framework and return scaffolding variables.

Here are the platform rules:
%s

DETECTION ALGORITHM & RESERVED NAMES:
The following framework names are RESERVED: 'react', 'expressjs', 'django', 'fastapi', 'java_springboot'.

Step 1: Evaluate the 'exact_match_templates' from the rules.
Step 2: If the project strictly matches one of the templates, the "framework" key MUST equal that exact reserved template string.
Step 3: If no exact template matches, you MUST NOT return a reserved name. Instead, return the actual framework or runtime name (e.g., 'nextjs', 'go', 'nestjs', 'python').
Step 4: Never invent template names. 

You MUST respond with ONLY valid JSON matching this exact structure:
{
  "framework": "string",
  "runtime": "string",
  "entry_path": "string (main entry file path relative to project root)",
  "app_port": number (the port this app listens on),
  "health_path": "string (health check HTTP path, e.g., /health, /ping)",
  "test_command": "string (command to run tests)",
  "test_image": "string (Docker image for running tests)",
  "run_command": "string (CMD for Dockerfile as JSON array string, e.g., '[\"./main\"]')"
}`, string(rulesData))

	userMessage := fmt.Sprintf("DETERMINISTIC FACTS:\nDetected package manager: %s\n\nAnalyze this project snapshot and return the JSON:\n\n%s", pkgManager, snapshot)

	responseText, err := client.Complete(systemPrompt, userMessage)
	if err != nil {
		return result, fmt.Errorf("AI detection failed: %w", err)
	}

	cleaned := strings.TrimSpace(responseText)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return result, fmt.Errorf("AI returned invalid JSON: %w\nRaw response: %s", err, responseText)
	}

	if result.Framework == "" {
		return result, fmt.Errorf("AI could not identify the framework")
	}
	if result.AppPort == 0 {
		result.AppPort = 8080 // safe fallback
	}

	fmt.Printf("\033[1;32m✓\033[0m AI identified framework: %s (port %d)\n", result.Framework, result.AppPort)
	return result, nil
}
