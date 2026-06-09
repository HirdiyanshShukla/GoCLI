package policies

import (
	"path/filepath"
	"strings"

	"devsandbox/core/policy"
)

// NoPrivilegedContainers checks that no Kubernetes manifest sets
// privileged: true or adds capabilities.
type NoPrivilegedContainers struct{}

func (p *NoPrivilegedContainers) Name() string        { return "no-privileged-containers" }
func (p *NoPrivilegedContainers) DisplayName() string { return "No Privileged Containers" }
func (p *NoPrivilegedContainers) Category() string    { return "security" }
func (p *NoPrivilegedContainers) Severity() string    { return "error" }
func (p *NoPrivilegedContainers) Description() string {
	return "Checks that no Kubernetes manifest sets privileged: true or capabilities.add in the container securityContext. Privileged containers have full host access and represent a critical security risk."
}

func (p *NoPrivilegedContainers) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	k8sChecked := false
	var findings []policy.Finding

	walkK8sFiles(projectPath, func(path string) {
		k8sChecked = true
		lines := readLines(path)
		if lines == nil {
			return
		}
		content := strings.Join(lines, "\n")
		if !strings.Contains(content, "kind: Deployment") {
			return
		}
		rel, _ := filepath.Rel(projectPath, path)

		for lineNum, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}

			// Check for privileged: true
			if strings.Contains(trimmed, "privileged: true") {
				findings = append(findings, policy.Finding{
					File:   rel,
					Line:   lineNum + 1,
					Detail: "privileged: true is set — containers must not run as privileged",
				})
			}

			// Check for capabilities.add: look for "capabilities:" line followed
			// by "add:" within the next 3 lines.
			if trimmed == "capabilities:" {
				window := lines[lineNum+1:]
				if len(window) > 3 {
					window = window[:3]
				}
				for _, wl := range window {
					if strings.TrimSpace(wl) == "add:" || strings.HasPrefix(strings.TrimSpace(wl), "add:") {
						findings = append(findings, policy.Finding{
							File:   rel,
							Line:   lineNum + 1,
							Detail: "capabilities.add is set — adding capabilities is not allowed; use only capabilities.drop",
						})
						break
					}
				}
			}
		}
	})

	if !k8sChecked {
		result.Findings = []policy.Finding{{Detail: "no k8s/ directory found — skipping privileged container checks"}}
		return result
	}

	if len(findings) > 0 {
		result.Passed = false
		result.Findings = findings
	}
	return result
}
