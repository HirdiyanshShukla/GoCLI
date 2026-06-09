package policies

import (
	"path/filepath"
	"strings"

	"devsandbox/core/policy"
)

// ResourceLimits ensures that Kubernetes Deployments define CPU and Memory limits
// to prevent noisy neighbor scenarios and cluster starvation.
type ResourceLimits struct{}

func (p *ResourceLimits) Name() string        { return "resource-limits" }
func (p *ResourceLimits) DisplayName() string { return "Resource Limits" }
func (p *ResourceLimits) Category() string    { return "reliability" }
func (p *ResourceLimits) Severity() string    { return "error" }
func (p *ResourceLimits) Description() string {
	return "Verifies that Kubernetes Deployments explicitly declare CPU and Memory requests and limits."
}

func (p *ResourceLimits) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	var findings []policy.Finding

	walkK8sFiles(projectPath, func(path string) {
		// 💡 THE FIX: Skip Kustomize patches
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

		// Basic string matching to ensure the resources block is defined
		if !strings.Contains(content, "resources:") {
			findings = append(findings, policy.Finding{
				File:   rel,
				Detail: "missing 'resources' block for container limits/requests",
			})
			return
		}

		requiredFields := []string{"limits:", "requests:", "cpu:", "memory:"}
		for _, field := range requiredFields {
			if !strings.Contains(content, field) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Detail: "missing resource definition field: " + field,
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