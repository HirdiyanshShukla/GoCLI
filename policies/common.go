package policies

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ignoreDirs matches the exact set used in scaffolding_engine/core/detector/detector.go
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
	"k8s":          true,
}

// walkSourceFiles visits every non-directory file under root, skipping ignoreDirs.
// The visitor receives the absolute path to each file. Errors from the visitor
// are silently swallowed — policies must never panic.
// walkSourceFiles visits every non-directory file under root, skipping ignoreDirs.
// The visitor receives the absolute path to each file. Errors from the visitor
// are silently swallowed — policies must never panic.
func walkSourceFiles(root string, visitor func(path string)) {
    // Define extensions to explicitly skip (binary files, images, etc.)
    ignoredExts := map[string]bool{
        ".exe": true,
        ".dll": true,
        ".so":  true,
        ".png": true,
        ".jpg": true,
        ".zip": true,
    }

    filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return nil
        }
        
        if d.IsDir() {
            if ignoreDirs[d.Name()] {
                return filepath.SkipDir
            }
            return nil
        }

        // Skip ignored file extensions to avoid reading compiled binaries
        ext := strings.ToLower(filepath.Ext(d.Name()))
        if ignoredExts[ext] {
            return nil
        }

        // Skip the devsandbox binary itself on Linux/Mac where it lacks an extension
        if d.Name() == "devsandbox" {
            return nil
        }

        visitor(path)
        return nil
    })
}

// readLines reads a file line-by-line. Returns nil if the file cannot be opened.
func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(data), "\n")
}

// isTestFile returns true if the path contains "test" in any component.
func isTestFile(path string) bool {
	lower := strings.ToLower(path)
	for _, part := range strings.Split(lower, string(filepath.Separator)) {
		if strings.Contains(part, "test") {
			return true
		}
	}
	return false
}

// detectFramework performs a lightweight framework identification based on
// the presence of key files in the project root. Used by policy implementations
// that need framework-aware scanning without importing the detector package.
func detectFramework(projectPath string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(projectPath, name))
		return err == nil
	}

	if exists("pom.xml") || exists("build.gradle") {
		return "java_springboot"
	}
	if exists("requirements.txt") {
		data, _ := os.ReadFile(filepath.Join(projectPath, "requirements.txt"))
		content := strings.ToLower(string(data))
		if strings.Contains(content, "django") {
			return "django"
		}
		if strings.Contains(content, "fastapi") {
			return "fastapi"
		}
		return "python"
	}
	if exists("manage.py") {
		return "django"
	}
	if exists("package.json") {
		data, _ := os.ReadFile(filepath.Join(projectPath, "package.json"))
		content := string(data)
		if strings.Contains(content, `"react"`) || strings.Contains(content, `"react-scripts"`) {
			return "react"
		}
		if strings.Contains(content, `"express"`) {
			return "expressjs"
		}
		return "node"
	}
	return "unknown"
}

// walkK8sFiles visits every .yml / .yaml file under the k8s/ subdirectory of
// projectPath, skipping template files (.tmpl). Unlike walkSourceFiles it does
// NOT apply ignoreDirs because directories such as build or dist are irrelevant
// in a K8s manifests tree.
func walkK8sFiles(projectPath string, visitor func(path string)) {
	k8sDir := filepath.Join(projectPath, "k8s")
	if _, err := os.Stat(k8sDir); os.IsNotExist(err) {
		return
	}
	filepath.WalkDir(k8sDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if (ext == ".yml" || ext == ".yaml") && !strings.HasSuffix(path, ".tmpl") {
			visitor(path)
		}
		return nil
	})
}
