package policies

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"devsandbox/core/policy"
)

// EnvVarSize flags environment variable values over 500 characters.
type EnvVarSize struct{}

func (p *EnvVarSize) Name() string        { return "env-var-size-limit" }
func (p *EnvVarSize) DisplayName() string { return "Environment Variable Size Limit" }
func (p *EnvVarSize) Category() string    { return "standards" }
func (p *EnvVarSize) Severity() string    { return "warning" }
func (p *EnvVarSize) Description() string {
	return "Flags environment variable values over 500 characters. Large env var values usually indicate someone is inlining a certificate, private key, or large config blob that should be a Kubernetes Secret or ConfigMap instead."
}

var envValuePattern = regexp.MustCompile(`(?i)^\s*value:\s*["\']?(.+?)["\']?\s*$`)

const envVarSizeLimit = 500

func (p *EnvVarSize) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	var findings []policy.Finding

	// --- Scan K8s manifests ---
	walkK8sFiles(projectPath, func(path string) {
		findings = append(findings, p.scanFile(projectPath, path)...)
	})

	// --- Also scan pipeline.yaml itself ---
	pipelineYaml := filepath.Join(projectPath, "pipeline.yaml")
	if _, err := os.Stat(pipelineYaml); err == nil {
		findings = append(findings, p.scanFile(projectPath, pipelineYaml)...)
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
