package generator

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"devsandbox/core/config"
	"devsandbox/scaffolding_engine/core/detector"
	"devsandbox/scaffolding_engine/templates"
)

func GenerateFiles(framework string, projectPath string, entryPath string, aiResult *detector.AIDetectionResult) error {
	userConfig, err := config.LoadConfig(projectPath)
	if err != nil {
		return fmt.Errorf("pipeline.yaml is malformed: %v", err)
	}

	rawName := filepath.Base(projectPath)
	re := regexp.MustCompile(`[^a-z0-9-]`)
	appName := strings.ToLower(rawName)
	appName = strings.ReplaceAll(appName, "_", "-")
	appName = strings.ReplaceAll(appName, " ", "-")
	appName = re.ReplaceAllString(appName, "")
	appName = strings.Trim(appName, "-")

	allDefaults := map[string]map[string]interface{}{
		"django": {
			"app_name":       appName,
			"app_port":       8000,
			"python_version": "3.12",
			"run_command":    fmt.Sprintf(`["python", "%s", "runserver", "0.0.0.0:8000"]`, entryPath),
			"health_path":    "/",
			"test_command":   fmt.Sprintf(`python %s test`, entryPath),
		},
		"fastapi": {
			"app_name":       appName,
			"app_port":       8000,
			"python_version": "3.12",
			"run_command":    fmt.Sprintf(`["uvicorn", "%s:app", "--host", "0.0.0.0", "--port", "8000"]`, entryPath),
			"health_path":    "/docs",
			"test_command":   `pytest`,
		},
		"expressjs": {
			"app_name":     appName,
			"app_port":     3000,
			"node_version": "22",
			"run_command":  `["npm", "start"]`,
			"health_path":  "/",
			"test_command": `npm run test`,
		},
		"react": {
			"app_name":     appName,
			"app_port":     8080,
			"node_version": "22",
			"run_command":  `["nginx", "-g", "daemon off;"]`,
			"health_path":  "/",
			"test_command": `npm run test`,
		},
		"java_springboot": {
			"app_name":     appName,
			"app_port":     8080,
			"java_version": "17",
			"run_command":  `["sh", "-c", "java -jar target/*.jar"]`,
			"health_path":  "/actuator/health",
			"test_command": `./mvnw test`,
		},
	}

	frameworkDefaults, ok := allDefaults[framework]
	if !ok {
		frameworkDefaults = map[string]interface{}{"app_name": appName}
	}

	finalVars := config.MergeWithDefaults(userConfig, frameworkDefaults)

	// --- ADD THIS SAFETY CHECK ---
	if finalVars["test_command"] == nil || finalVars["test_command"] == "" {
		finalVars["test_command"] = "echo 'No tests defined'"
	}

	// ── 3a. Inject AI-detected variables if present ───────────────────────────
	if aiResult != nil {
		// Only override port, health, test, and run command from AI.
		// For known frameworks, version variables (python_version, node_version, etc.)
		// stay as defaults/user-overrides so templates render correctly.
		if aiResult.AppPort != 0 {
			finalVars["app_port"] = aiResult.AppPort
		}
		if aiResult.HealthPath != "" {
			finalVars["health_path"] = aiResult.HealthPath
		}
		if aiResult.TestCommand != "" {
			finalVars["test_command"] = aiResult.TestCommand
		}
		if aiResult.RunCommand != "" {
			finalVars["run_command"] = aiResult.RunCommand
		}
		// framework key used by Jenkinsfile template if needed
		finalVars["framework"] = aiResult.Framework
	}

	// ── 3b. Compute test_image — always from finalVars for consistency ────────
	// For known frameworks this ensures the test container matches the Dockerfile version.
	// For unknown frameworks, fall back to AI-provided test_image.
	switch framework {
	case "django", "fastapi":
		finalVars["test_image"] = fmt.Sprintf("python:%v-slim", finalVars["python_version"])
	case "expressjs", "react", "node":
		finalVars["test_image"] = fmt.Sprintf("node:%v-alpine", finalVars["node_version"])
	case "java_springboot":
		finalVars["test_image"] = fmt.Sprintf("maven:%v-eclipse-temurin", finalVars["java_version"])
	default:
		// Unknown framework — use AI-provided test_image
		if aiResult != nil && aiResult.TestImage != "" {
			finalVars["test_image"] = aiResult.TestImage
		}
	}

	// --- ABSOLUTE SAFETY FALLBACK ---
	// If the AI returns an empty image (e.g., no tests found), default to alpine
	// so the pipeline doesn't crash on a <no value> template evaluation.
	if finalVars["test_image"] == nil || finalVars["test_image"] == "" {
		finalVars["test_image"] = "alpine:3.19"
	}

	// ADD THIS: Ensure health_path never goes to the template as nil
	if finalVars["health_path"] == nil || finalVars["health_path"] == "" {
		finalVars["health_path"] = "/"
	}

	// ── 4. Generate Dockerfile via AI only if no template exists ─────────────
	// Known frameworks (react, django, etc.) already have Dockerfile.tmpl — skip AI.
	// Only call AIGenerateDockerfile for truly unknown frameworks.
	if aiResult != nil {
		dockerfileTemplatePath := framework + "/Dockerfile.tmpl"
		_, templateErr := templates.Files.Open(dockerfileTemplatePath)
		hasDockerfileTemplate := templateErr == nil

		if !hasDockerfileTemplate {
			aiDockerfile, err := AIGenerateDockerfile(projectPath, finalVars)
			if err != nil {
				return fmt.Errorf("AI Dockerfile generation failed: %w", err)
			}
			dockerfilePath := filepath.Join(projectPath, "Dockerfile")
			if _, statErr := os.Stat(dockerfilePath); statErr != nil {
				if err := os.WriteFile(dockerfilePath, []byte(aiDockerfile.DockerfileContent), 0644); err != nil {
					return fmt.Errorf("failed to write AI Dockerfile: %w", err)
				}
				fmt.Println("Generated", dockerfilePath)
			} else {
				fmt.Printf("⚠️  Skipping existing file (already customized): Dockerfile\n")
			}
		}
	}

	// ── 5. Walk framework-specific templates first, then shared ───────────────
	var dirsToWalk []string
	if _, err := templates.Files.Open(framework); err == nil {
		dirsToWalk = []string{framework, "shared"}
	} else {
		dirsToWalk = []string{"shared"}
	}

	for _, dir := range dirsToWalk {
		err := fs.WalkDir(templates.Files, dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			if d.IsDir() {
				return nil
			}

			if strings.HasSuffix(d.Name(), ".tmpl") {
				relPath := strings.TrimPrefix(path, dir+"/")
				outputRelPath := strings.TrimSuffix(relPath, ".tmpl")
				destPath := filepath.Join(projectPath, outputRelPath)

				if err = os.MkdirAll(filepath.Dir(destPath), os.ModePerm); err != nil {
					return err
				}

				fileData, err := templates.Files.ReadFile(path)
				if err != nil {
					return err
				}

				if _, err := os.Stat(destPath); err == nil {
					if !(aiResult != nil && outputRelPath == "Dockerfile") {
						fmt.Printf("⚠️  Skipping existing file (already customized): %s\n", outputRelPath)
					}
					return nil
				}

				tmpl, err := template.New(filepath.Base(path)).Parse(string(fileData))
				if err != nil {
					return err
				}

				outFile, err := os.Create(destPath)
				if err != nil {
					return err
				}
				defer outFile.Close()

				if err = tmpl.Execute(outFile, finalVars); err != nil {
					return fmt.Errorf("error executing template %s: %w", path, err)
				}

				fmt.Println("Generated", destPath)
			}
			return nil
		})

		if err != nil {
			return err
		}
	}

	return nil
}
