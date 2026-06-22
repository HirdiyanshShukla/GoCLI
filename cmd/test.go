// File: cmd/test.go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"devsandbox/core"
	"devsandbox/core/config"
	"devsandbox/core/policy"
	"devsandbox/policies"
	"devsandbox/scaffolding_engine/core/detector"

	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Runs comprehensive local validation (Code, Docker, K8s, Security, and Policies)",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("\033[1;36m🔍 Commencing Comprehensive Local Validation...\033[0m")

		fmt.Println("\n\033[1;34m📋 Stage 1: Running Code Quality Linters...\033[0m")
		codeLintOK := lintCode()

		fmt.Println("\n\033[1;34m🧪 Stage 2: Executing Unit Test Suites...\033[0m")
		testsOK := unitTests()

		fmt.Println("\n\033[1;34m🐳 Stage 3: Linting Dockerfile...\033[0m")
		dockerOK := lintDocker()

		fmt.Println("\n\033[1;34m☸️  Stage 4: Validating Kubernetes Manifests...\033[0m")
		k8sOK := lintK8s()

		fmt.Println("\n\033[1;34m🔒 Stage 5: Running Security Scans...\033[0m")
		securityOK := securityScan()

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

		// Aggregate all failures and print a deterministic summary
		if policyFailed || !codeLintOK || !testsOK || !dockerOK || !k8sOK || !securityOK {
			fmt.Println("\n\033[1;31m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")
			fmt.Println("\033[1;31m❌ VALIDATION FAILED: Action Required\033[0m")
			fmt.Println("\033[1;31m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")

			if !codeLintOK {
				fmt.Println(" 👉 \033[33mCode Quality:\033[0m Fix the linter errors printed in Stage 1.")
			}
			if !testsOK {
				fmt.Println(" 👉 \033[33mUnit Tests:\033[0m Fix the failing tests printed in Stage 2.")
			}
			if !dockerOK {
				fmt.Println(" 👉 \033[33mDockerfile:\033[0m Resolve the Hadolint warnings printed in Stage 3.")
			}
			if !k8sOK {
				fmt.Println(" 👉 \033[33mKubernetes:\033[0m Fix the Kubeconform validation errors in Stage 4.")
			}
			if !securityOK {
				fmt.Println(" 👉 \033[33mSecurity:\033[0m Address the Checkov IaC vulnerabilities in Stage 5.")
			}
			if policyFailed {
				fmt.Println(" 👉 \033[33mPlatform Policies:\033[0m Review the Policy Check table above to resolve compliance issues.")
			}

			fmt.Println("\n\033[36mRun 'devsandbox validate' again after applying fixes.\033[0m")
			os.Exit(1)
		}
		fmt.Println("\n\033[1;32m✅ Complete Local Validation Successful! Codebase is secure, compliant, and ready for deployment.\033[0m")
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func getTestImage(cwd, ecosystem string) (image string, isMaven bool, isGradle bool) {
	cfg, _ := config.LoadConfig(cwd)
	switch ecosystem {
	case "python":
		v := "3.12"
		if cfg.App.PythonVersion != "" {
			v = cfg.App.PythonVersion
		}
		return fmt.Sprintf("python:%s-slim", v), false, false
	case "node":
		v := "22"
		if cfg.App.NodeVersion != "" {
			v = cfg.App.NodeVersion
		}
		return fmt.Sprintf("node:%s-alpine", v), false, false
	case "java":
		v := "17"
		if cfg.App.JavaVersion != "" {
			v = cfg.App.JavaVersion
		}
		if _, err := os.Stat(filepath.Join(cwd, "pom.xml")); err == nil {
			return fmt.Sprintf("maven:3.9-eclipse-temurin-%s-alpine", v), true, false
		}
		return fmt.Sprintf("eclipse-temurin:%s-alpine", v), false, true
	case "go":
		return "golang:1.22-alpine", false, false
	case "rust":
		return "rust:1-slim", false, false
	case "ruby":
		return "ruby:3.3-alpine", false, false
	}
	return "", false, false
}

func lintCode() bool {
	cwd, _ := os.Getwd()
	ecosystem := detector.DetectEcosystem(cwd)
	appName := core.SanitizeForDocker(filepath.Base(cwd))

	switch ecosystem {
	case "node":
		if !detector.HasNpmScript(cwd, "lint") {
			fmt.Println("ℹ️  No 'lint' script in package.json. Skipping code linting.")
			return true
		}
		image, _, _ := getTestImage(cwd, ecosystem)
		pm := detector.DetectPackageManager(cwd)
		installCmd, runCmd := "npm ci --silent --no-audit --no-fund", "npm run lint --silent"

		switch pm {
		case "pnpm":
			installCmd = "npm install -g pnpm --silent --no-audit --no-fund && pnpm install --frozen-lockfile --silent"
			runCmd = "pnpm run lint"
		case "yarn":
			installCmd = "npm install -g yarn --silent --no-audit --no-fund && yarn install --frozen-lockfile --silent"
			runCmd = "yarn lint"
		case "bun":
			installCmd = "npm install -g bun --silent --no-audit --no-fund && bun install --frozen-lockfile --silent"
			runCmd = "bun run lint"
		}
		return core.RunInContainer("Code Linting", image, installCmd, runCmd,
			"devsandbox-npm-cache-"+appName, "/root/.npm")

	case "python":
		image, _, _ := getTestImage(cwd, ecosystem)
		return core.RunInContainer("Code Linting", image,
			"pip install --quiet --disable-pip-version-check --break-system-packages flake8", "flake8 .",
			"devsandbox-pip-cache-"+appName, "/root/.cache/pip")

	case "go":
		image, _, _ := getTestImage(cwd, ecosystem)
		return core.RunInContainer("Code Linting", image, "", "go vet ./...",
			"devsandbox-go-cache-"+appName, "/root/.cache/go-build")

	case "rust":
		image, _, _ := getTestImage(cwd, ecosystem)
		return core.RunInContainer("Code Linting", image,
			"rustup component add clippy --quiet 2>/dev/null || true", "cargo clippy",
			"devsandbox-cargo-cache-"+appName, "/usr/local/cargo/registry")

	case "java":
		image, isMaven, isGradle := getTestImage(cwd, ecosystem)
		if isMaven {
			return core.RunInContainer("Code Linting", image, "", "mvn checkstyle:check -B -q",
				"devsandbox-mvn-cache-"+appName, "/root/.m2")
		}
		if isGradle {
			if _, err := os.Stat(filepath.Join(cwd, "gradlew")); err != nil {
				fmt.Println("ℹ️  No gradlew wrapper found. Skipping code linting for Gradle.")
				return true
			}
			return core.RunInContainer("Code Linting", image, "",
				"chmod +x ./gradlew && ./gradlew check -q",
				"devsandbox-gradle-cache-"+appName, "/root/.gradle")
		}
		return true

	default:
		fmt.Println("ℹ️  No default linter configured for this ecosystem. Skipping code linting.")
		return true
	}
}

func findDjangoEntry(cwd string) string {
	if _, err := os.Stat(filepath.Join(cwd, "manage.py")); err == nil {
		return "manage.py"
	}
	return ""
}

func unitTests() bool {
	cwd, _ := os.Getwd()
	ecosystem := detector.DetectEcosystem(cwd)
	appName := core.SanitizeForDocker(filepath.Base(cwd))

	switch ecosystem {
	case "node":
		if !detector.HasNpmScript(cwd, "test") {
			fmt.Println("\033[1;33m⚠️  CRITICAL WARNING: No 'test' script found in package.json.\033[0m")
			fmt.Println("\033[1;33m   Unit tests are highly recommended before production deployment.\033[0m")
			return true 
		}
		image, _, _ := getTestImage(cwd, ecosystem)
		pm := detector.DetectPackageManager(cwd)
		installCmd, runCmd := "npm ci --silent --no-audit --no-fund", "npm test"

		switch pm {
		case "pnpm":
			installCmd = "npm install -g pnpm --silent --no-audit --no-fund && pnpm install --frozen-lockfile --silent"
			runCmd = "pnpm test"
		case "yarn":
			installCmd = "npm install -g yarn --silent --no-audit --no-fund && yarn install --frozen-lockfile --silent"
			runCmd = "yarn test"
		case "bun":
			installCmd = "npm install -g bun --silent --no-audit --no-fund && bun install --frozen-lockfile --silent"
			runCmd = "bun test"
		}
		return core.RunInContainer("Unit Testing", image, installCmd, runCmd,
			"devsandbox-npm-cache-"+appName, "/root/.npm")

	case "python":
		image, _, _ := getTestImage(cwd, ecosystem)
		if entry := findDjangoEntry(cwd); entry != "" {
			return core.RunInContainer("Unit Testing", image, "", "python3 "+entry+" test",
				"devsandbox-pip-cache-"+appName, "/root/.cache/pip")
		}
		return core.RunInContainer("Unit Testing", image,
			"pip install --quiet --disable-pip-version-check --break-system-packages pytest", "pytest",
			"devsandbox-pip-cache-"+appName, "/root/.cache/pip")

	case "go":
		image, _, _ := getTestImage(cwd, ecosystem)
		return core.RunInContainer("Unit Testing", image, "", "go test ./...",
			"devsandbox-go-cache-"+appName, "/root/.cache/go-build")

	case "rust":
		image, _, _ := getTestImage(cwd, ecosystem)
		return core.RunInContainer("Unit Testing", image, "", "cargo test",
			"devsandbox-cargo-cache-"+appName, "/usr/local/cargo/registry")

	case "java":
		image, isMaven, isGradle := getTestImage(cwd, ecosystem)
		if isMaven {
			return core.RunInContainer("Unit Testing", image, "", "mvn test -B -q",
				"devsandbox-mvn-cache-"+appName, "/root/.m2")
		}
		if isGradle {
			if _, err := os.Stat(filepath.Join(cwd, "gradlew")); err != nil {
				fmt.Println("ℹ️  No gradlew wrapper found. Skipping tests for Gradle.")
				return true
			}
			return core.RunInContainer("Unit Testing", image, "",
				"chmod +x ./gradlew && ./gradlew test -q",
				"devsandbox-gradle-cache-"+appName, "/root/.gradle")
		}
		return true

	default:
		return runCustomTestCommand(cwd)
	}
}

func runCustomTestCommand(cwd string) bool {
	fmt.Println("ℹ️  Custom framework detected. Consulting pipeline.yaml contract...")

	yamlPath := filepath.Join(cwd, "pipeline.yaml")
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		cliName := filepath.Base(os.Args[0])
		fmt.Printf("\033[1;31m❌ pipeline.yaml missing. Please execute '%s init' first.\033[0m\n", cliName)
		return false
	}

	userConfig, err := config.LoadConfig(cwd)
	if err != nil {
		fmt.Printf("\033[1;31m❌ Configuration Error: %s\033[0m\n", err.Error())
		return false
	}

	extractedCmd := userConfig.App.TestCommand
	if extractedCmd == "" || extractedCmd == "your-test-command" || extractedCmd == "echo 'No tests defined'" {
		fmt.Println("\033[1;33m⚠️  No custom validation or test_command found in pipeline.yaml. Skipping code test layer.\033[0m")
		return true
	}

	return core.RunInContainer("Unit Testing", "alpine:3.19", "", extractedCmd, "", "")
}

func lintDocker() bool {
	project := core.AnalyzeProject()
	cwd, _ := os.Getwd()
	if project["has_docker"] {
		return core.ExecCommandTracked("Hadolint Docker Check", "docker", "run", "--rm", "-v", fmt.Sprintf("%s:/work", core.ToDockerPath(cwd)), "-w", "/work", "hadolint/hadolint", "hadolint", "--ignore", "DL3018", "Dockerfile")
	}
	fmt.Println("No Dockerfile found. Skipping.")
	return true
}

func lintK8s() bool {
	project := core.AnalyzeProject()
	cwd, _ := os.Getwd()
	if project["has_k8s"] {
		overlayPath := filepath.Join(cwd, "k8s/overlays/local")
		if _, err := os.Stat(overlayPath); os.IsNotExist(err) {
			cliName := filepath.Base(os.Args[0])
			fmt.Printf("\033[33m⚠️  No Kustomize overlays found. Please run '%s init' first to generate manifests.\033[0m\n", cliName)
			return false
		}

		tempFile, _ := os.CreateTemp("", "k8s-dump-*.yaml")
		defer os.Remove(tempFile.Name())

		kustomizeCmd := exec.Command("kubectl", "kustomize", overlayPath)
		output, _ := kustomizeCmd.Output()
		tempFile.Write(output)
		tempFile.Close()

		return core.ExecCommandTracked("Kubeconform K8s Validation", "docker", "run", "--rm", "-v", fmt.Sprintf("%s:/manifest.yaml", core.ToDockerPath(tempFile.Name())), "ghcr.io/yannh/kubeconform:latest", "-strict", "-summary", "/manifest.yaml")
	}
	fmt.Println("No Kubernetes manifests found. Skipping.")
	return true
}

func securityScan() bool {
	project := core.AnalyzeProject()
	cwd, _ := os.Getwd()
	if project["has_docker"] || project["has_k8s"] {
		return core.ExecCommandTracked("Checkov Security Audit", "docker", "run", "--rm", "-v", fmt.Sprintf("%s:/work", core.ToDockerPath(cwd)), "bridgecrew/checkov", "-d", "/work", "--framework", "dockerfile", "kubernetes", "github_actions", "--skip-check", "CKV_K8S_14,CKV_K8S_43,CKV2_K8S_6,CKV2_GHA_1,CKV_K8S_40,CKV_K8S_31", "--skip-path", "env", "--skip-path", "venv", "--skip-path", "node_modules", "--skip-path", ".git", "--skip-path", "k8s/overlays", "--quiet", "--compact")
	}
	fmt.Println("No infrastructure files found for security scan. Skipping.")
	return true
}