package detector

import (
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
