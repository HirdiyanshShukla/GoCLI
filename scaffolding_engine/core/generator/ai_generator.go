package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devsandbox/core/ai"
	"devsandbox/scaffolding_engine/core/rules"
)

// AIGeneratorResult holds the output of AI Dockerfile generation.
type AIGeneratorResult struct {
	DockerfileContent string
}

// AIGenerateDockerfile calls Gemini to generate a Dockerfile
// for an unknown framework, constrained by rules.yaml platform standards.
func AIGenerateDockerfile(projectPath string, vars map[string]interface{}) (AIGeneratorResult, error) {
	var result AIGeneratorResult

	client, err := ai.NewClient()
	if err != nil {
		return result, err
	}
	defer client.Close()

	rulesData, err := rules.Files.ReadFile("rules.yaml")
	if err != nil {
		return result, fmt.Errorf("failed to read rules.yaml: %w", err)
	}

	existingDockerfile := ""
	existingPath := filepath.Join(projectPath, "Dockerfile")
	if data, err := os.ReadFile(existingPath); err == nil {
		existingDockerfile = fmt.Sprintf("\n=== EXISTING DOCKERFILE (use as reference only) ===\n%s", string(data))
	}

	systemPrompt := fmt.Sprintf(`You are a platform engineering assistant generating a production-grade Dockerfile.
You MUST follow these platform standards exactly — they are non-negotiable security and reliability requirements.
Pay extreme attention to the "prompt_instructions.dockerfile_generation_phase" and "framework_profiles" sections:

%s

Additional mandatory requirements:
1. Use multi-stage builds when the runtime requires compilation (Go, Java, Rust, .NET)
2. Final image must run as a non-root user (UID 1000)
3. If using Alpine-based images, install curl before the HEALTHCHECK: RUN apk add --no-cache curl
4. If using Debian/slim images, curl is already available
5. HEALTHCHECK must use the exact health_path provided — do not substitute a different path
6. EXPOSE must use the exact port provided
7. Use only approved base images from the approved_base_images list in the rules
8. readOnlyRootFilesystem is enforced by Kubernetes — your Dockerfile must not write outside /tmp at runtime
9. The dockerfile_content value must be a valid JSON string — all double quotes inside the Dockerfile must be escaped as \", all backslashes as \\.
Respond with ONLY the raw Dockerfile content wrapped in a JSON object with the key "dockerfile_content".`, string(rulesData))

	framework, _ := vars["framework"].(string)
	appPort := vars["app_port"]
	healthPath, _ := vars["health_path"].(string)
	runCommand, _ := vars["run_command"].(string)
	testCommand, _ := vars["test_command"].(string)

	userMessage := fmt.Sprintf(`Generate a production Dockerfile for this project.
Framework: %s
Port: %v
Health check path: %s
Run command (for CMD): %s
Test command: %s

%s`, framework, appPort, healthPath, runCommand, testCommand, existingDockerfile)

	fmt.Println("\033[1;36m🤖 Generating Dockerfile for unknown framework...\033[0m")

	responseJSON, err := client.Complete(systemPrompt, userMessage)
	if err != nil {
		return result, fmt.Errorf("AI Dockerfile generation failed: %w", err)
	}

	cleaned := strings.TrimSpace(responseJSON)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")

	var parsed struct {
		Content string `json:"dockerfile_content"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return result, fmt.Errorf("failed to parse AI Dockerfile response: %w", err)
	}

	if !strings.HasPrefix(strings.TrimSpace(parsed.Content), "FROM") {
		return result, fmt.Errorf("AI returned invalid Dockerfile (does not start with FROM)")
	}

	result.DockerfileContent = strings.TrimSpace(parsed.Content)
	fmt.Println("\033[1;32m✓\033[0m AI generated Dockerfile successfully")
	return result, nil
}
