package policies

import (
	"path/filepath"
	"strings"

	"devsandbox/core/policy"
)

type NonRootContainer struct{}

func (p *NonRootContainer) Name() string        { return "non-root-container" }
func (p *NonRootContainer) DisplayName() string { return "Non-Root Container" }
func (p *NonRootContainer) Category() string    { return "security" }
func (p *NonRootContainer) Severity() string    { return "error" }
func (p *NonRootContainer) Description() string {
	return "Ensures that all containers are configured to run as a non-root user via Dockerfile USER directives and K8s securityContext restrictions."
}

func (p *NonRootContainer) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	var findings []policy.Finding

	// --- 1. Scan Dockerfiles ---
	walkSourceFiles(projectPath, func(path string) {
		if filepath.Base(path) != "Dockerfile" {
			return
		}
		lines := readLines(path)
		if lines == nil {
			return
		}

		rel, _ := filepath.Rel(projectPath, path)
		hasUserDirective := false

		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToUpper(trimmed), "USER ") {
				userVal := strings.TrimSpace(trimmed[5:])
				if strings.ToLower(userVal) == "root" || userVal == "0" {
					findings = append(findings, policy.Finding{
						File:   rel,
						Detail: "Dockerfile explicitly sets USER root — container will run as root",
					})
				}
				hasUserDirective = true
			}
		}

		if !hasUserDirective {
			findings = append(findings, policy.Finding{
				File:   rel,
				Detail: "Dockerfile has no USER directive — container defaults to running as root",
			})
		}
	})

	// --- 2. Scan K8s Manifests ---
	walkK8sFiles(projectPath, func(path string) {
		normalizedPath := filepath.ToSlash(path)
		if strings.Contains(normalizedPath, "/k8s/overlays/") || strings.HasPrefix(normalizedPath, "k8s/overlays/") {
			return
		}

		lines := readLines(path)
		if lines == nil {
			return
		}

		content := strings.Join(lines, "\n")
		if !strings.Contains(content, "kind: Deployment") {
			return
		}

		rel, _ := filepath.Rel(projectPath, path)

		requiredFields := []string{
			"runAsNonRoot: true",
			"runAsUser:",
			"allowPrivilegeEscalation: false",
		}

		for _, field := range requiredFields {
			if !strings.Contains(content, field) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Detail: "missing securityContext field: " + field,
				})
			}
		}
	})

	if len(findings) > 0 {
		result.Passed = false
		result.Findings = findings
	}
	return result
}
