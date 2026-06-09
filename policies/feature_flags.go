package policies

import (
	"os"
	"path/filepath"
	"strings"

	"devsandbox/core/policy"
)

// FeatureFlags verifies that a feature flag library is declared in dependencies
// AND actually imported somewhere in source code.
type FeatureFlags struct{}

func (p *FeatureFlags) Name() string        { return "feature-flags" }
func (p *FeatureFlags) DisplayName() string { return "Feature Flags" }
func (p *FeatureFlags) Category() string    { return "standards" }
func (p *FeatureFlags) Severity() string    { return "error" }
func (p *FeatureFlags) Description() string {
	return "Ensures a feature flag library is declared in dependencies and actively imported in source code. Defaults to checking for flagsmith, launchdarkly-server-sdk, or openfeature."
}

var defaultFlagLibraries = []string{"flagsmith", "launchdarkly-server-sdk", "openfeature"}

func (p *FeatureFlags) Run(projectPath string, config map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	// Determine which library to look for
	var libraries []string
	if ffConfig, ok := config["feature-flags"]; ok {
		if lib, ok := ffConfig["library"].(string); ok && lib != "" {
			libraries = []string{lib}
		}
	}
	if len(libraries) == 0 {
		libraries = defaultFlagLibraries
	}

	framework := detectFramework(projectPath)

	// Check if any library is declared in the dependency file
	inDeps, foundLib := p.checkDependencies(projectPath, framework, libraries)
	if !inDeps {
		result.Passed = false
		result.Message = "No feature flag library found in dependencies. Add one of: " + strings.Join(libraries, ", ")
		return result
	}

	// Check if the found library is actually imported in source
	imported := p.checkImport(projectPath, foundLib)
	if !imported {
		result.Passed = false
		result.Findings = []policy.Finding{{
			Detail: foundLib + " is declared but never used in source code.",
		}}
	}

	return result
}

func (p *FeatureFlags) checkDependencies(projectPath, framework string, libraries []string) (bool, string) {
	switch framework {
	case "django", "fastapi", "python":
		lines := readLines(filepath.Join(projectPath, "requirements.txt"))
		for _, line := range lines {
			lower := strings.ToLower(line)
			for _, lib := range libraries {
				if strings.Contains(lower, strings.ToLower(lib)) {
					return true, lib
				}
			}
		}
	case "expressjs", "react", "node":
		data, err := os.ReadFile(filepath.Join(projectPath, "package.json"))
		if err == nil {
			content := strings.ToLower(string(data))
			for _, lib := range libraries {
				if strings.Contains(content, strings.ToLower(lib)) {
					return true, lib
				}
			}
		}
	case "java_springboot":
		data, err := os.ReadFile(filepath.Join(projectPath, "pom.xml"))
		if err == nil {
			content := strings.ToLower(string(data))
			for _, lib := range libraries {
				if strings.Contains(content, strings.ToLower(lib)) {
					return true, lib
				}
			}
		}
	}
	return false, ""
}

func (p *FeatureFlags) checkImport(projectPath, library string) bool {
	found := false
	walkSourceFiles(projectPath, func(path string) {
		if found {
			return
		}
		lines := readLines(path)
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), strings.ToLower(library)) {
				found = true
				return
			}
		}
	})
	return found
}
