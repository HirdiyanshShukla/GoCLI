package policies

import (
	"fmt"
	"path/filepath"
	"strings"

	"devsandbox/core/policy"
)

// ApprovedRegistries ensures all container images come from approved registries.
type ApprovedRegistries struct{}

func (p *ApprovedRegistries) Name() string        { return "approved-registries" }
func (p *ApprovedRegistries) DisplayName() string { return "Approved Container Registries" }
func (p *ApprovedRegistries) Category() string    { return "security" }
func (p *ApprovedRegistries) Severity() string    { return "error" }
func (p *ApprovedRegistries) Description() string {
	return "Ensures all container images in Dockerfiles and Kubernetes manifests are pulled from approved registries only. Prevents supply-chain attacks from untrusted image sources. Configure allowed registries in pipeline.yaml."
}

var defaultApprovedRegistries = []string{
	"127.0.0.1:5001",     // local Kind registry
	"docker.io/library",  // official Docker Hub images (nginx, python, node, etc.)
	"docker.io",          // Docker Hub user images
	"gcr.io",
	"ghcr.io",
	"mcr.microsoft.com",
	"registry.k8s.io",
	"public.ecr.aws",
}

func (p *ApprovedRegistries) Run(projectPath string, config map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	// Determine the approved registry list.
	approved := defaultApprovedRegistries
	if len(config) > 0 {
		if arConfig, ok := config["approved-registries"]; ok {
			if allowed, ok := arConfig["allowed"]; ok {
				if list, ok := allowed.([]interface{}); ok {
					var custom []string
					for _, item := range list {
						if s, ok := item.(string); ok {
							custom = append(custom, s)
						}
					}
					if len(custom) > 0 {
						approved = custom
					}
				}
			}
		}
	}

	var findings []policy.Finding

	// --- Scan Dockerfiles ---
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
			if !strings.HasPrefix(strings.ToUpper(trimmed), "FROM ") {
				continue
			}
			parts := strings.Fields(trimmed)
			if len(parts) < 2 {
				continue
			}
			image := parts[1]
			if strings.EqualFold(image, "scratch") {
				continue
			}
			registry := extractRegistry(image)
			if !isApproved(registry, approved) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Line:   lineNum + 1,
					Detail: fmt.Sprintf("image %q uses unapproved registry %q", image, registry),
				})
			}
		}
	})

	// --- Scan K8s manifests ---
	walkK8sFiles(projectPath, func(path string) {
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
			if strings.Contains(line, "{{") {
				continue
			}
			if !strings.Contains(trimmed, "image:") {
				continue
			}
			image := strings.TrimSpace(strings.SplitN(trimmed, "image:", 2)[1])
			image = strings.Trim(image, "'\"")
			if image == "" {
				continue
			}
			registry := extractRegistry(image)
			if !isApproved(registry, approved) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Line:   lineNum + 1,
					Detail: fmt.Sprintf("image %q uses unapproved registry %q", image, registry),
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

// extractRegistry derives the canonical registry identifier from an image reference.
//
// Rules (mirrors Docker's own resolution):
//   - "nginx" or "nginx:alpine"     → docker.io/library  (no slash → official image)
//   - "user/image:tag"              → docker.io           (slash, but prefix has no . or :)
//   - "gcr.io/project/image:tag"    → gcr.io
//   - "127.0.0.1:5001/app:sha"      → 127.0.0.1:5001
func extractRegistry(image string) string {
	slashIdx := strings.Index(image, "/")
	if slashIdx == -1 {
		// No slash → official Docker Hub image (e.g. "nginx", "python")
		return "docker.io/library"
	}
	prefix := image[:slashIdx]
	// If the prefix contains a dot or colon it is a registry hostname.
	if strings.ContainsAny(prefix, ".:") {
		return prefix
	}
	// Otherwise it is a Docker Hub username (e.g. "user/image") → docker.io
	return "docker.io"
}

// isApproved returns true if registry starts with any approved registry string.
func isApproved(registry string, approved []string) bool {
	for _, a := range approved {
		if strings.HasPrefix(registry, a) || strings.HasPrefix(a, registry) {
			return true
		}
	}
	return false
}
