package policies

import (
	"path/filepath"
	"strings"

	"devsandbox/core/policy"
)

// MandatoryProbes checks that every Kubernetes Deployment manifest defines
// both a readinessProbe and a livenessProbe.
type MandatoryProbes struct{}

func (p *MandatoryProbes) Name() string        { return "mandatory-probes" }
func (p *MandatoryProbes) DisplayName() string { return "Mandatory Kubernetes Probes" }
func (p *MandatoryProbes) Category() string    { return "reliability" }
func (p *MandatoryProbes) Severity() string    { return "error" }
func (p *MandatoryProbes) Description() string {
	return "Checks that every Kubernetes Deployment manifest defines both a readinessProbe and a livenessProbe. Missing probes cause silent failures and unstable rolling updates."
}

func (p *MandatoryProbes) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	var findings []policy.Finding

	walkK8sFiles(projectPath, func(path string) {
		// 💡 THE FIX: Skip overlay patches — they are intentionally partial manifests.
		// Full security context and probes live in k8s/base/ only.
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

		probes := []string{"readinessProbe:", "livenessProbe:"}
		for _, probe := range probes {
			if !strings.Contains(content, probe) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Detail: "missing probe: " + probe,
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