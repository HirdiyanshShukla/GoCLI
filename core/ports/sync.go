package ports

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var registryHostPattern = regexp.MustCompile(`127\.0\.0\.1:\d+`)

// SyncRegistryToProject rewrites registry host references in generated pipeline files.
func SyncRegistryToProject(projectPath, registryHost string) error {
	targets := []string{
		filepath.Join(projectPath, "Jenkinsfile"),
	}

	_ = filepath.WalkDir(filepath.Join(projectPath, "k8s"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yml" || ext == ".yaml" {
			targets = append(targets, path)
		}
		return nil
	})

	for _, path := range targets {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}

		updated := registryHostPattern.ReplaceAllString(string(data), registryHost)
		if updated == string(data) {
			continue
		}

		if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
			return err
		}
	}

	return nil
}
