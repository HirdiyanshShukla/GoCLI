package policies

import (
	"path/filepath"
	"strings"

	"devsandbox/core/policy"
)

// HealthEndpoint checks that the project exposes a health check route.
type HealthEndpoint struct{}

func (p *HealthEndpoint) Name() string        { return "health-endpoint" }
func (p *HealthEndpoint) DisplayName() string { return "Health Endpoint" }
func (p *HealthEndpoint) Category() string    { return "reliability" }
func (p *HealthEndpoint) Severity() string    { return "error" }
func (p *HealthEndpoint) Description() string {
	return "Verifies that the application exposes a health check endpoint. Framework-aware: checks Django URLs, FastAPI routes, Express routes, and Spring Boot actuator. React projects are skipped."
}

func (p *HealthEndpoint) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	framework := detectFramework(projectPath)

	// React frontend apps don't need a health endpoint
	if framework == "react" {
		return result
	}

	found := false

	switch framework {
	case "django":
		// Look for a URL pattern containing "health" in any urls.py
		walkSourceFiles(projectPath, func(path string) {
			if found {
				return
			}
			if filepath.Base(path) == "urls.py" {
				lines := readLines(path)
				for _, line := range lines {
					if strings.Contains(line, "health") {
						found = true
						return
					}
				}
			}
		})

	case "fastapi", "python":
		// Look for @app.get or @router.get with /health or /healthz
		walkSourceFiles(projectPath, func(path string) {
			if found {
				return
			}
			if !strings.HasSuffix(path, ".py") {
				return
			}
			lines := readLines(path)
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if (strings.Contains(trimmed, "@app.get") || strings.Contains(trimmed, "@router.get")) &&
					(strings.Contains(trimmed, "/health") || strings.Contains(trimmed, "/healthz")) {
					found = true
					return
				}
			}
		})

	case "expressjs", "node":
		// Look for app.get with /health
		walkSourceFiles(projectPath, func(path string) {
			if found {
				return
			}
			if !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".ts") {
				return
			}
			lines := readLines(path)
			for _, line := range lines {
				if strings.Contains(line, "app.get") && strings.Contains(line, "/health") {
					found = true
					return
				}
			}
		})

	case "java_springboot":
		// Look for /actuator/health or @GetMapping with health in the path
		walkSourceFiles(projectPath, func(path string) {
			if found {
				return
			}
			if !strings.HasSuffix(path, ".java") {
				return
			}
			lines := readLines(path)
			for _, line := range lines {
				if strings.Contains(line, "/actuator/health") ||
					(strings.Contains(line, "@GetMapping") && strings.Contains(line, "health")) {
					found = true
					return
				}
			}
		})
	}

	if !found {
		result.Passed = false
		result.Message = "No health endpoint found. Add a /health or /healthz route to your application."
	}
	return result
}
