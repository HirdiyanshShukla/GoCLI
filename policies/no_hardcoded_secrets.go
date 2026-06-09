package policies

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"devsandbox/core/policy"
)

// NoHardcodedSecrets scans source files and pipeline.yaml for credentials
// assigned to string literals.
type NoHardcodedSecrets struct{}

func (p *NoHardcodedSecrets) Name() string        { return "no-hardcoded-secrets" }
func (p *NoHardcodedSecrets) DisplayName() string { return "No Hardcoded Secrets" }
func (p *NoHardcodedSecrets) Category() string    { return "security" }
func (p *NoHardcodedSecrets) Severity() string    { return "error" }
func (p *NoHardcodedSecrets) Description() string {
	return "Scans all source files and pipeline.yaml for hardcoded passwords, tokens, API keys, and other credentials assigned to string literal values."
}

// secretVarPattern matches variable assignments where the variable name is
// a known secret word and the value is a non-empty string literal.
var secretVarPattern = regexp.MustCompile(
	`(?i)(password|passwd|secret|api_key|apikey|token|private_key|auth_token)\s*[=:]\s*["']([^"']+)["']`,
)

func (p *NoHardcodedSecrets) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	var findings []policy.Finding

	// Collect all source files + pipeline.yaml
	var filesToScan []string
	walkSourceFiles(projectPath, func(path string) {
		filesToScan = append(filesToScan, path)
	})
	// Always scan pipeline.yaml regardless of ignoreDirs
	pipelineYaml := filepath.Join(projectPath, "pipeline.yaml")
	if _, err := os.Stat(pipelineYaml); err == nil {
		filesToScan = append(filesToScan, pipelineYaml)
	}

	for _, filePath := range filesToScan {
		lines := readLines(filePath)
		if lines == nil {
			continue
		}
		rel, _ := filepath.Rel(projectPath, filePath)
		for lineNum, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Skip comment lines
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			matches := secretVarPattern.FindStringSubmatch(line)
			if len(matches) >= 3 {
				varName := matches[1]
				findings = append(findings, policy.Finding{
					File:   rel,
					Line:   lineNum + 1,
					Detail: fmt.Sprintf("variable matches secret pattern: %s", varName),
				})
			}
		}
	}

	if len(findings) > 0 {
		result.Passed = false
		result.Findings = findings
	}
	return result
}
