package policies

import (
	"path/filepath"
	"regexp"
	"strings"

	"devsandbox/core/policy"
)

// ApiVersioning checks that all defined routes include a version prefix (v1/, v2/, etc.).
type ApiVersioning struct{}

func (p *ApiVersioning) Name() string        { return "api-versioning" }
func (p *ApiVersioning) DisplayName() string { return "API Versioning" }
func (p *ApiVersioning) Category() string    { return "standards" }
func (p *ApiVersioning) Severity() string    { return "warning" }
func (p *ApiVersioning) Description() string {
	return "Ensures all API routes are versioned (e.g. /v1/users). Framework-aware: scans Django urls.py, FastAPI decorators, Express route handlers, and Spring Boot mapping annotations."
}

// versionedRoute matches paths that start with /v<digit> or v<digit>/ anywhere in the string
var versionedRoute = regexp.MustCompile(`[/"']/?v\d+[/"]`)

func (p *ApiVersioning) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	framework := detectFramework(projectPath)

	var findings []policy.Finding

	switch framework {
	case "django":
		findings = p.scanDjango(projectPath)
	case "fastapi", "python":
		findings = p.scanFastAPI(projectPath)
	case "expressjs", "node":
		findings = p.scanExpress(projectPath)
	case "react":
		// Frontend apps don't define API routes
		return result
	case "java_springboot":
		findings = p.scanSpringBoot(projectPath)
	}

	if len(findings) > 0 {
		result.Passed = false
		result.Findings = findings
	}
	return result
}

// routePathPattern extracts the path string argument from a route definition.
var djangoRouteRe = regexp.MustCompile(`(?:path|re_path)\s*\(\s*["'](.*?)["']`)
var fastapiRouteRe = regexp.MustCompile(`@(?:app|router)\.\w+\s*\(\s*["'](.*?)["']`)
var expressRouteRe = regexp.MustCompile(`(?:app|router)\.(?:get|post|put|patch|delete|use)\s*\(\s*["'](.*?)["']`)
var springRouteRe = regexp.MustCompile(`@(?:RequestMapping|GetMapping|PostMapping|PutMapping|DeleteMapping|PatchMapping)\s*\(\s*(?:value\s*=\s*)?["'](.*?)["']`)

func (p *ApiVersioning) scanDjango(projectPath string) []policy.Finding {
	var findings []policy.Finding
	walkSourceFiles(projectPath, func(path string) {
		if filepath.Base(path) != "urls.py" {
			return
		}
		lines := readLines(path)
		rel, _ := filepath.Rel(projectPath, path)
		for lineNum, line := range lines {
			matches := djangoRouteRe.FindStringSubmatch(line)
			if len(matches) < 2 {
				continue
			}
			route := matches[1]
			if !isVersioned(route) && !isStaticOrAdmin(route) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Line:   lineNum + 1,
					Detail: "/" + strings.TrimPrefix(route, "/") + " — missing version prefix",
				})
			}
		}
	})
	return findings
}

func (p *ApiVersioning) scanFastAPI(projectPath string) []policy.Finding {
	var findings []policy.Finding
	walkSourceFiles(projectPath, func(path string) {
		if !strings.HasSuffix(path, ".py") {
			return
		}
		lines := readLines(path)
		rel, _ := filepath.Rel(projectPath, path)
		for lineNum, line := range lines {
			matches := fastapiRouteRe.FindStringSubmatch(line)
			if len(matches) < 2 {
				continue
			}
			route := matches[1]
			if !isVersioned(route) && !isStaticOrAdmin(route) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Line:   lineNum + 1,
					Detail: route + " — missing version prefix",
				})
			}
		}
	})
	return findings
}

func (p *ApiVersioning) scanExpress(projectPath string) []policy.Finding {
	var findings []policy.Finding
	walkSourceFiles(projectPath, func(path string) {
		if !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".ts") {
			return
		}
		lines := readLines(path)
		rel, _ := filepath.Rel(projectPath, path)
		for lineNum, line := range lines {
			matches := expressRouteRe.FindStringSubmatch(line)
			if len(matches) < 2 {
				continue
			}
			route := matches[1]
			if !isVersioned(route) && !isStaticOrAdmin(route) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Line:   lineNum + 1,
					Detail: route + " — missing version prefix",
				})
			}
		}
	})
	return findings
}

func (p *ApiVersioning) scanSpringBoot(projectPath string) []policy.Finding {
	var findings []policy.Finding
	walkSourceFiles(projectPath, func(path string) {
		if !strings.HasSuffix(path, ".java") {
			return
		}
		lines := readLines(path)
		rel, _ := filepath.Rel(projectPath, path)
		for lineNum, line := range lines {
			matches := springRouteRe.FindStringSubmatch(line)
			if len(matches) < 2 {
				continue
			}
			route := matches[1]
			if !isVersioned(route) && !isStaticOrAdmin(route) {
				findings = append(findings, policy.Finding{
					File:   rel,
					Line:   lineNum + 1,
					Detail: route + " — missing version prefix",
				})
			}
		}
	})
	return findings
}

func isVersioned(route string) bool {
	return versionedRoute.MatchString(route)
}

func isStaticOrAdmin(route string) bool {
	lower := strings.ToLower(route)
	return lower == "" || lower == "/" ||
		strings.HasPrefix(lower, "static") ||
		strings.HasPrefix(lower, "admin") ||
		strings.HasPrefix(lower, "health") ||
		strings.HasPrefix(lower, "docs") ||
		strings.HasPrefix(lower, "redoc") ||
		strings.HasPrefix(lower, "openapi")
}
