package detector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

var ignoreDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	"venv":         true,
	".venv":        true,
	"env":          true,
	".env":         true,
	"__pycache__":  true,
	".cache":       true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
}

// DetectPackageManager deterministically scans for package managers
func DetectPackageManager(projectPath string) string {
	lockfiles := map[string]string{
		"pnpm-lock.yaml":    "pnpm",
		"yarn.lock":         "yarn",
		"bun.lockb":         "bun",
		"package-lock.json": "npm",
	}

	for file, manager := range lockfiles {
		if _, err := os.Stat(filepath.Join(projectPath, file)); err == nil {
			return manager
		}
	}
	return "unknown or N/A"
}

// DetectFramework is a fast, offline static detector used only for local
// validation and lint command routing where AI calls would be wasteful.
func DetectFramework(projectPath string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(projectPath, name))
		return err == nil
	}
	readLower := func(name string) string {
		data, err := os.ReadFile(filepath.Join(projectPath, name))
		if err != nil {
			return ""
		}
		return strings.ToLower(string(data))
	}

	if exists("pom.xml") || exists("build.gradle") {
		return "java_springboot"
	}
	if exists("requirements.txt") {
		content := readLower("requirements.txt")
		if strings.Contains(content, "django") || exists("manage.py") {
			return "django"
		}
		if strings.Contains(content, "fastapi") {
			return "fastapi"
		}
	}
	if exists("manage.py") {
		return "django"
	}
	if exists("package.json") {
		content := readLower("package.json")
		if strings.Contains(content, `"react"`) {
			return "react"
		}
		if strings.Contains(content, `"express"`) {
			return "expressjs"
		}
	}
	return "unknown"
}


// DetectEcosystem identifies the broad language/runtime ecosystem
func DetectEcosystem(projectPath string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(projectPath, name))
		return err == nil
	}

	switch {
	case exists("package.json"):
		return "node"
	case exists("requirements.txt") || exists("pyproject.toml") || exists("Pipfile"):
		return "python"
	case exists("go.mod"):
		return "go"
	case exists("Cargo.toml"):
		return "rust"
	case exists("pom.xml") || exists("build.gradle") || exists("build.gradle.kts"):
		return "java"
	case exists("Gemfile"):
		return "ruby"
	case exists("composer.json"):
		return "php"
	}
	return "unknown"
}

// HasNpmScript checks if a specific script exists in package.json
func HasNpmScript(projectPath, scriptName string) bool {
	data, err := os.ReadFile(filepath.Join(projectPath, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	_, ok := pkg.Scripts[scriptName]
	return ok
}
