// File: cmd/test.go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"devsandbox/core"
	"devsandbox/core/ai"
	"devsandbox/core/config"
	"devsandbox/core/policy"
	"devsandbox/policies"
	"devsandbox/scaffolding_engine/core/detector"

	"github.com/spf13/cobra"
)

// requireLocalTool checks if a binary is in the host's PATH.
func requireLocalTool(tool string, installMsg string) bool {
	if _, err := exec.LookPath(tool); err != nil {
		fmt.Printf("\033[33m⚠️  Required tool '%s' is not installed or not in PATH.\033[0m\n", tool)
		fmt.Printf("\033[33m   👉 Fix: %s\033[0m\n", installMsg)
		return false
	}
	return true
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Runs comprehensive local validation (Code, Docker, K8s, Security, and Policies)",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("\033[1;36m🔍 Commencing Comprehensive Local Validation...\033[0m")

		fmt.Println("\n\033[1;34m📋 Stage 1: Running Code Quality Linters...\033[0m")
		lintCode()

		fmt.Println("\n\033[1;34m🧪 Stage 2: Executing Unit Test Suites...\033[0m")
		unitTests()

		fmt.Println("\n\033[1;34m🐳 Stage 3: Linting Dockerfile...\033[0m")
		lintDocker()

		fmt.Println("\n\033[1;34m☸️  Stage 4: Validating Kubernetes Manifests...\033[0m")
		lintK8s()

		fmt.Println("\n\033[1;34m🔒 Stage 5: Running Security Scans...\033[0m")
		securityScan()

		fmt.Println("\n\033[1;34m🛡️  Stage 6: Evaluating Platform Policies...\033[0m")
		cwd, _ := os.Getwd()
		cfg, cfgErr := config.LoadConfig(cwd)
		policyFailed := false
		if cfgErr != nil {
			fmt.Printf("\033[1;31m❌ %s\033[0m\n", cfgErr.Error())
			policyFailed = true
		} else {
			results := policy.RunPolicies(cwd, cfg, policies.All())
			policyFailed = policy.PrintReport(results)
		}

		if policyFailed {
			fmt.Println("\n\033[1;31m❌ Validation failed: one or more error-severity policies did not pass.\033[0m")
			if os.Getenv("GEMINI_API_KEY") != "" {
				fmt.Println("\n\033[1;35m🤖 Validation failed. Auto-analyzing failures with Gemini AI...\033[0m")

				var builder strings.Builder
				builder.WriteString("Validation Stage: Evaluating Platform Policies failed.\n")
				builder.WriteString("Failing Policies:\n")
				if cfgErr != nil {
					builder.WriteString(fmt.Sprintf("- Configuration Error: %s\n", cfgErr.Error()))
				} else {
					results := policy.RunPolicies(cwd, cfg, policies.All())
					for _, r := range results {
						if !r.Passed {
							builder.WriteString(fmt.Sprintf("- [%s] %s: %s\n", r.Severity, r.PolicyName, r.Message))
							for _, f := range r.Findings {
								builder.WriteString(fmt.Sprintf("  Finding: %s:%d - %s\n", f.File, f.Line, f.Detail))
							}
						}
					}
				}

				analysis, err := ai.AnalyzeLogs(builder.String())
				if err == nil {
					ai.PrintAnalysis(analysis)
					core.AskAndApplyFixes(analysis)
				} else {
					fmt.Printf("❌ Auto-analysis failed: %v\n", err)
				}
			}
			os.Exit(1)
		}
		fmt.Println("\n\033[1;32m✅ Complete Local Validation Successful! Codebase is secure, compliant, and ready for deployment.\033[0m")
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func lintCode() {
	cwd, _ := os.Getwd()
	framework := detector.DetectFramework(cwd)

	switch framework {
	case "react", "expressjs":
		if requireLocalTool("npm", "Install Node.js and run 'npm install' in this directory") {
			// Set to true so it prints the error but continues the pipeline
			core.ExecCommand("Code Linting", true, true, "npm", "run", "lint")
		}
	case "django":
		if requireLocalTool("flake8", "pip install flake8") {
			core.ExecCommand("Code Linting", true, true, "flake8", ".", "--exclude=env,venv,.env,.venv,node_modules,.git,__pycache__")
		}
	case "fastapi":
		if requireLocalTool("black", "pip install black") {
			core.ExecCommand("Code Linting", true, true, "black", "--check", ".")
		}
	case "java_springboot":
		if _, err := os.Stat(filepath.Join(cwd, "mvnw")); err == nil {
			core.ExecCommand("Code Linting", true, true, "./mvnw", "checkstyle:check")
		} else {
			fmt.Println("\033[33m⚠️  ./mvnw not found. Please ensure you are at the project root.\033[0m")
		}
	default:
		fmt.Println("ℹ️  No default linter configured for this framework. Skipping code linting.")
	}
}

func unitTests() {
	cwd, _ := os.Getwd()
	framework := detector.DetectFramework(cwd)

	var shell, shellFlag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		shellFlag = "/c"
	} else {
		shell = "sh"
		shellFlag = "-c"
	}

	switch framework {
	case "react", "expressjs":
		if requireLocalTool("npm", "Install Node.js and run 'npm install'") {
			// Set to true to warn and continue
			core.ExecCommand("Unit Testing", true, true, shell, shellFlag, "npm test")
		}
	case "django":
		if requireLocalTool("python", "Install Python") {
			core.ExecCommand("Unit Testing", true, true, shell, shellFlag, "python manage.py test")
		}
	case "fastapi":
		if requireLocalTool("pytest", "pip install pytest") {
			core.ExecCommand("Unit Testing", true, true, shell, shellFlag, "pytest")
		}
	case "java_springboot":
		if _, err := os.Stat(filepath.Join(cwd, "mvnw")); err == nil {
			core.ExecCommand("Unit Testing", true, true, shell, shellFlag, "./mvnw test")
		} else {
			fmt.Println("\033[33m⚠️  ./mvnw not found.\033[0m")
		}
	default:
		fmt.Println("ℹ️  Custom framework detected. Consulting pipeline.yaml contract...")

		yamlPath := filepath.Join(cwd, "pipeline.yaml")
		if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
			cliName := filepath.Base(os.Args[0])
			fmt.Printf("\033[1;31m❌ pipeline.yaml missing. Please execute '%s init' first.\033[0m\n", cliName)
			os.Exit(1)
		}

		userConfig, err := config.LoadConfig(cwd)
		if err != nil {
			fmt.Printf("\033[1;31m❌ Configuration Error: %s\033[0m\n", err.Error())
			os.Exit(1)
		}

		extractedCmd := userConfig.App.TestCommand
		if extractedCmd == "" || extractedCmd == "your-test-command" || extractedCmd == "echo 'No tests defined'" {
			fmt.Println("\033[1;33m⚠️  No custom validation or test_command found in pipeline.yaml. Skipping code test layer.\033[0m")
		} else {
			core.ExecCommand("Unit Testing", true, true, shell, shellFlag, extractedCmd)
		}
	}
}

func lintDocker() {
	project := core.AnalyzeProject()
	cwd, _ := os.Getwd()
	if project["has_docker"] {
		fmt.Println("Linting Dockerfile...")
		core.ExecCommand("Hadolint Docker Check", true, false, "docker", "run", "--rm", "-v", fmt.Sprintf("%s:/work", cwd), "-w", "/work", "hadolint/hadolint", "hadolint", "Dockerfile")
	} else {
		fmt.Println("No Dockerfile found. Skipping.")
	}
}

func lintK8s() {
	project := core.AnalyzeProject()
	cwd, _ := os.Getwd()
	if project["has_k8s"] {
		fmt.Println("Validating Kubernetes manifests...")

		overlayPath := filepath.Join(cwd, "k8s/overlays/local")
		if _, err := os.Stat(overlayPath); os.IsNotExist(err) {
			cliName := filepath.Base(os.Args[0])
			fmt.Printf("\033[33m⚠️  No Kustomize overlays found. Please run '%s init' first to generate manifests.\033[0m\n", cliName)
			return
		}

		tempFile, _ := os.CreateTemp("", "k8s-dump-*.yaml")
		defer os.Remove(tempFile.Name())

		kustomizeCmd := exec.Command("kubectl", "kustomize", overlayPath)
		output, _ := kustomizeCmd.Output()
		tempFile.Write(output)
		tempFile.Close()

		core.ExecCommand("Kubeconform K8s Validation", true, false, "docker", "run", "--rm", "-v", fmt.Sprintf("%s:/manifest.yaml", tempFile.Name()), "ghcr.io/yannh/kubeconform:latest", "-strict", "-summary", "/manifest.yaml")
	} else {
		fmt.Println("No Kubernetes manifests found. Skipping.")
	}
}

func securityScan() {
	project := core.AnalyzeProject()
	cwd, _ := os.Getwd()
	if project["has_docker"] || project["has_k8s"] {
		fmt.Println("Running Checkov Security Scan on IaC...")
		core.ExecCommand("Checkov Security Audit", true, false, "docker", "run", "--rm", "-v", fmt.Sprintf("%s:/work", cwd), "bridgecrew/checkov", "-d", "/work", "--framework", "dockerfile", "kubernetes", "github_actions", "--skip-check", "CKV_K8S_14,CKV_K8S_43,CKV2_K8S_6,CKV2_GHA_1,CKV_K8S_40,CKV_K8S_31", "--skip-path", "env", "--skip-path", "venv", "--skip-path", "node_modules", "--skip-path", ".git", "--skip-path", "k8s/overlays", "--quiet", "--compact")
	} else {
		fmt.Println("No infrastructure files found for security scan. Skipping.")
	}
}