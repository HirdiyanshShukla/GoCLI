package policies

import (
	"fmt"
	"path/filepath"
	"strings"

	"devsandbox/core/policy"
)

// NoLatestTag ensures no Dockerfile or Kubernetes manifest uses the :latest
// image tag (or an untagged image, which implicitly pulls :latest).
type NoLatestTag struct{}

func (p *NoLatestTag) Name() string        { return "no-latest-tag" }
func (p *NoLatestTag) DisplayName() string { return "No Latest Image Tag" }
func (p *NoLatestTag) Category() string    { return "security" }
func (p *NoLatestTag) Severity() string    { return "error" }
func (p *NoLatestTag) Description() string {
	return "Ensures no Dockerfile or Kubernetes manifest uses the :latest image tag. Latest tags cause unpredictable deployments because the image can change silently."
}

func (p *NoLatestTag) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	var findings []policy.Finding

	// --- Scan Dockerfiles via walkSourceFiles ---
	walkSourceFiles(projectPath, func(path string) {
		if filepath.Base(path) != "Dockerfile" {
			return
		}
		lines := readLines(path)
		if lines == nil {
			return
		}
		rel, _ := filepath.Rel(projectPath, path)
		for lineNum, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			// Only interested in FROM instructions
			if !strings.HasPrefix(strings.ToUpper(trimmed), "FROM ") {
				continue
			}
			// FROM scratch is always valid
			parts := strings.Fields(trimmed)
			if len(parts) < 2 {
				continue
			}
			image := parts[1]
			if strings.EqualFold(image, "scratch") {
				continue
			}
			// Skip digest references: image@sha256:...
			if strings.Contains(image, "@sha256:") {
				continue
			}
			if isLatestOrUntagged(image) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Line:   lineNum + 1,
					Detail: fmt.Sprintf("FROM %s — uses :latest or has no tag", image),
				})
			}
		}
	})

	// --- Scan K8s YAML manifests via walkK8sFiles ---
	walkK8sFiles(projectPath, func(path string) {
		
		// 💡 THE FIX: Skip base manifests — they intentionally use :latest as a Kustomize placeholder.
		// We use ToSlash to handle Windows paths safely, and check both relative and absolute patterns.
		normalizedPath := filepath.ToSlash(path)
		if strings.Contains(normalizedPath, "/k8s/base/") || strings.HasPrefix(normalizedPath, "k8s/base/") {
			return
		}

		lines := readLines(path)
		if lines == nil {
			return
		}
		rel, _ := filepath.Rel(projectPath, path)
		for lineNum, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			// Skip Go template placeholder lines
			if strings.Contains(line, "{{") {
				continue
			}
			if !strings.Contains(trimmed, "image:") {
				continue
			}
			// Extract image reference: trim "image:" and surrounding whitespace/quotes
			image := strings.TrimSpace(strings.SplitN(trimmed, "image:", 2)[1])
			image = strings.Trim(image, "'\"")
			if image == "" {
				continue
			}
			if strings.Contains(image, "@sha256:") {
				continue
			}
			if isLatestOrUntagged(image) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Line:   lineNum + 1,
					Detail: fmt.Sprintf("image: %s — uses :latest or has no tag", image),
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

// isLatestOrUntagged returns true if an image reference ends with :latest or
// contains no tag at all (no colon in the image name portion, excluding
// registry hostnames like localhost:5001 or 127.0.0.1:5001).
func isLatestOrUntagged(image string) bool {
	if strings.HasSuffix(image, ":latest") {
		return true
	}
	// Strip registry prefix before checking for a tag.
	// Registry prefixes contain dots or colons in the first path component.
	name := image
	slashIdx := strings.Index(image, "/")
	if slashIdx != -1 {
		prefix := image[:slashIdx]
		// If the prefix looks like a hostname (contains . or :), strip it.
		if strings.ContainsAny(prefix, ".:") {
			name = image[slashIdx+1:]
		}
	}
	// Now name is like "nginx", "library/nginx", "my-org/my-app:1.2.3".
	// If there is no : in name, the image has no explicit tag → implicitly latest.
	if !strings.Contains(name, ":") {
		return true
	}
	return false
}