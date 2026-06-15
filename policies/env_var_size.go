package policies

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"devsandbox/core/policy"
)

type EnvVarSize struct{}

func (p *EnvVarSize) Name() string        { return "env-var-size-limit" }
func (p *EnvVarSize) DisplayName() string { return "Environment Variable Size Limit" }
func (p *EnvVarSize) Category() string    { return "standards" }
func (p *EnvVarSize) Severity() string    { return "warning" }
func (p *EnvVarSize) Description() string {
	return "Flags environment variable values over 500 characters. Large env var values usually indicate someone is inlining a certificate, private key, or large config blob."
}

var envValuePattern = regexp.MustCompile(`(?i)^\s*value:\s*["']?(.+?)["']?\s*$`)

const envVarSizeLimit = 500

func (p *EnvVarSize) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	var findings []policy.Finding

	// --- 1. Scan K8s manifests ---
	walkK8sFiles(projectPath, func(path string) {
		normalizedPath := filepath.ToSlash(path)
		if strings.Contains(normalizedPath, "/k8s/overlays/") || strings.HasPrefix(normalizedPath, "k8s/overlays/") {
			return
		}
		findings = append(findings, p.scanFile(projectPath, path)...)
	})

	// --- 2. Scan pipeline.yaml itself ---
	pipelineYaml := filepath.Join(projectPath, "pipeline.yaml")
	if _, err := os.Stat(pipelineYaml); err == nil {
		findings = append(findings, p.scanFile(projectPath, pipelineYaml)...)
	}

	// --- 3. Scan .env files ---
	envFiles := []string{".env", ".env.local", ".env.production", ".env.development"}
	for _, envFile := range envFiles {
		envPath := filepath.Join(projectPath, envFile)
		if _, err := os.Stat(envPath); err == nil {
			findings = append(findings, p.scanEnvFile(projectPath, envPath)...)
		}
	}

	if len(findings) > 0 {
		result.Passed = false
		result.Findings = findings
	}
	return result
}

func (p *EnvVarSize) scanFile(projectPath, path string) []policy.Finding {
	lines := readLines(path)
	if lines == nil {
		return nil
	}
	rel, _ := filepath.Rel(projectPath, path)
	var findings []policy.Finding
	for lineNum, line := range lines {
		matches := envValuePattern.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue
		}
		value := matches[1]
		if len(value) > envVarSizeLimit {
			preview := value
			if len(preview) > 40 {
				preview = preview[:40]
			}
			findings = append(findings, policy.Finding{
				File:   rel,
				Line:   lineNum + 1,
				Detail: fmt.Sprintf("env var value is %d chars (limit %d): %s...", len(value), envVarSizeLimit, preview),
			})
		}
	}
	return findings
}

func (p *EnvVarSize) scanEnvFile(projectPath, path string) []policy.Finding {
	lines := readLines(path)
	if lines == nil {
		return nil
	}
	rel, _ := filepath.Rel(projectPath, path)
	var findings []policy.Finding
	for lineNum, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "=") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 && len(parts[1]) > envVarSizeLimit {
			findings = append(findings, policy.Finding{
				File:   rel,
				Line:   lineNum + 1,
				Detail: fmt.Sprintf("env var %s value is %d chars (limit %d)", parts[0], len(parts[1]), envVarSizeLimit),
			})
		}
	}
	return findings
}
