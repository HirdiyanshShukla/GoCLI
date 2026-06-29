// File: cmd/test.go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"devsandbox/core"
	"devsandbox/core/ai"
	"devsandbox/core/config"
	"devsandbox/core/finops"
	"devsandbox/core/policy"
	"devsandbox/policies"
	"devsandbox/scaffolding_engine/core/detector"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Runs comprehensive local validation (Code, Docker, K8s, Security, and Policies)",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("\033[1;36mCommencing Comprehensive Local Validation...\033[0m")

		fmt.Println("\n\033[1;34mStage 1: Running Code Quality Linters...\033[0m")
		codeLintOK := lintCode()

		fmt.Println("\n\033[1;34mStage 2: Executing Unit Test Suites...\033[0m")
		testsOK := unitTests()

		fmt.Println("\n\033[1;34mStage 3: Linting Dockerfile...\033[0m")
		dockerOK := lintDocker()

		fmt.Println("\n\033[1;34m\u26a0\ufe0f  Stage 4: Validating Kubernetes Manifests...\033[0m")
		k8sOK := lintK8s()

		fmt.Println("\n\033[1;34mStage 5: Running Security Scans...\033[0m")
		securityOK := securityScan()

		fmt.Println("\n\033[1;34m\u26a0\ufe0f  Stage 6: Evaluating Platform Policies...\033[0m")
		cwd, _ := os.Getwd()
		cfg, cfgErr := config.LoadConfig(cwd)
		policyFailed := false
		if cfgErr != nil {
			fmt.Printf("\033[1;31m\u274c %s\033[0m\n", cfgErr.Error())
			policyFailed = true
		} else {
			results := policy.RunPolicies(cwd, cfg, policies.All())
			policyFailed = policy.PrintReport(results)
		}

		fmt.Println("\n\U0001f4ca Stage 7: FinOps Budget Predictor...")
		finopsCheck()

		// Aggregate all failures and print a deterministic summary
		if policyFailed || !codeLintOK || !testsOK || !dockerOK || !k8sOK || !securityOK {
			fmt.Println("\n\033[1;31m\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\033[0m")
			fmt.Println("\033[1;31m\u274c VALIDATION FAILED: Action Required\033[0m")
			fmt.Println("\033[1;31m\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\u2501\033[0m")

			if !codeLintOK {
				fmt.Println(" \033[33mCode Quality:\033[0m Fix the linter errors printed in Stage 1.")
			}
			if !testsOK {
				fmt.Println(" \033[33mUnit Tests:\033[0m Fix the failing tests printed in Stage 2.")
			}
			if !dockerOK {
				fmt.Println(" \033[33mDockerfile:\033[0m Resolve the Hadolint warnings printed in Stage 3.")
			}
			if !k8sOK {
				fmt.Println(" \033[33mKubernetes:\033[0m Fix the Kubeconform validation errors in Stage 4.")
			}
			if !securityOK {
				fmt.Println(" \033[33mSecurity:\033[0m Address the Checkov IaC vulnerabilities in Stage 5.")
			}
			if policyFailed {
				fmt.Println(" \033[33mPlatform Policies:\033[0m Review the Policy Check table above to resolve compliance issues.")
			}

			fmt.Println("\n\033[36mRun 'validate command' again after applying fixes.\033[0m")
			os.Exit(1)
		}

		fmt.Println("\n\033[1;32m\u2705 Complete Local Validation Successful! Codebase is secure, compliant, and ready for deployment.\033[0m")
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func getTestImage(cwd, ecosystem string) (image string, isMaven bool, isGradle bool) {
	cfg, _ := config.LoadConfig(cwd)
	switch ecosystem {
	case "node":
		nodeVer := cfg.App.NodeVersion
		if nodeVer == "" {
			nodeVer = "22"
		}
		return "node:" + nodeVer + "-alpine", false, false
	case "python":
		pyVer := cfg.App.PythonVersion
		if pyVer == "" {
			pyVer = "3.12"
		}
		return "python:" + pyVer + "-slim", false, false
	case "go":
		return "golang:1.24-alpine", false, false
	case "rust":
		return "rust:alpine", false, false
	case "java":
		javaVer := cfg.App.JavaVersion
		if javaVer == "" {
			javaVer = "17"
		}
		_, maven := func() (bool, bool) {
			_, errM := os.Stat(filepath.Join(cwd, "pom.xml"))
			_, errG := os.Stat(filepath.Join(cwd, "build.gradle"))
			return errM == nil, errG == nil
		}()
		if maven {
			return "maven:" + javaVer + "-eclipse-temurin-" + javaVer, true, false
		}
		return "gradle:jdk" + javaVer, false, true
	}
	return "alpine:3.19", false, false
}

func lintCode() bool {
	cwd, _ := os.Getwd()
	ecosystem := detector.DetectEcosystem(cwd)
	appName := core.SanitizeForDocker(filepath.Base(cwd))

	switch ecosystem {
	case "node":
		if !detector.HasNpmScript(cwd, "lint") {
			fmt.Println("\u2139\ufe0f  No 'lint' script found in package.json. Skipping code linting.")
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
			"pip install --quiet --disable-pip-version-check --break-system-packages flake8",
			"flake8 --exclude=venv,.venv,env,.env,node_modules,.git,__pycache__ .",
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
				fmt.Println("\u2139\ufe0f  No gradlew wrapper found. Skipping code linting for Gradle.")
				return true
			}
			return core.RunInContainer("Code Linting", image, "",
				"chmod +x ./gradlew && ./gradlew check -q",
				"devsandbox-gradle-cache-"+appName, "/root/.gradle")
		}
		return true

	default:
		fmt.Println("\u2139\ufe0f  No default linter configured for this ecosystem. Skipping code linting.")
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
			fmt.Println("\033[1;33m\u26a0\ufe0f  CRITICAL WARNING: No 'test' script found in package.json.\033[0m")
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
				fmt.Println("\u2139\ufe0f  No gradlew wrapper found. Skipping tests for Gradle.")
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
	fmt.Println("\u2139\ufe0f  Custom framework detected. Consulting pipeline.yaml contract...")

	yamlPath := filepath.Join(cwd, "pipeline.yaml")
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		cliName := filepath.Base(os.Args[0])
		fmt.Printf("\033[1;31m\u274c pipeline.yaml missing. Please execute '%s init' first.\033[0m\n", cliName)
		return false
	}

	userConfig, err := config.LoadConfig(cwd)
	if err != nil {
		fmt.Printf("\033[1;31m\u274c Configuration Error: %s\033[0m\n", err.Error())
		return false
	}

	extractedCmd := userConfig.App.TestCommand
	if extractedCmd == "" || extractedCmd == "your-test-command" || extractedCmd == "echo 'No tests defined'" {
		fmt.Println("\033[1;33m\u26a0\ufe0f  No custom validation or test_command found in pipeline.yaml. Skipping code test layer.\033[0m")
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
			fmt.Printf("\033[33m\u26a0\ufe0f  No Kustomize overlays found. Please run '%s init' first to generate manifests.\033[0m\n", cliName)
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

// finopsCheck is Stage 7 - advisory only, never calls os.Exit.
// It renders the prod Kustomize overlay, estimates monthly cost using hardcoded
// on-demand rates, and optionally asks Gemini for structural optimizations.
func finopsCheck() {
	cwd, _ := os.Getwd()
	if !core.AnalyzeProject()["has_k8s"] {
		return // no K8s manifests - nothing to estimate
	}

	manifestYAML, err := finops.RenderProdManifest(cwd)
	if err != nil {
		fmt.Println("\u2139\ufe0f  Could not render prod overlay for cost estimate. Skipping.")
		return
	}

	containers, replicas, _ := finops.ParseContainerResources(manifestYAML)
	if len(containers) == 0 {
		return
	}
	totals := finops.SumResources(containers, replicas)

	cfg, _ := config.LoadConfig(cwd)
	region := cfg.App.FinOpsRegion // "" falls back to "default" rates inside MonthlyCost
	currentCost := finops.MonthlyCost(totals, region)

	fmt.Printf("\n\033[1;34m\U0001f4ca Estimated Monthly Cost: $%.2f\033[0m\n", currentCost)
	fmt.Println("\033[90m   Estimate based on generic on-demand pricing; actual cost varies by provider, region, and commitment tier.\033[0m")

	hash := finops.HashTotals(totals)
	cached, found := finops.LoadCache(cwd)
	if found && cached.Hash == hash {
		fmt.Println("\033[90m   (cached - resource allocations unchanged since last check)\033[0m")
		if len(cached.Mutations) > 0 {
			fmt.Println("\033[1;33m\U0001f4a1 Previously found optimizations (unchanged):\033[0m")
			for _, m := range cached.Mutations {
				fmt.Printf("   \u2022 [%s] %s: %s -> %s\n", m.ContainerName, m.FieldPath, m.OldValue, m.NewValue)
			}
			fmt.Println("\033[90m   Run with --finops-refresh to re-analyze, or apply manually.\033[0m")
		} else {
			fmt.Println("\033[1;32m\u2713\033[0m No cost optimizations found (cached).")
		}
		return
	}

	var names []string
	for _, c := range containers {
		names = append(names, c.Name)
	}
	framework := detector.DetectFramework(cwd) // reuse existing static detector

	mutations, err := ai.AnalyzeFinOps(manifestYAML, framework, names)
	if err != nil {
		fmt.Printf("\033[33m\u26a0\ufe0f  FinOps AI analysis unavailable: %v\033[0m\n", err)
		finops.SaveCache(cwd, finops.CacheEntry{Hash: hash, CurrentCost: currentCost})
		return
	}

	if len(mutations) == 0 {
		fmt.Println("\033[1;32m\u2713\033[0m No cost optimizations found - allocations look reasonable.")
		finops.SaveCache(cwd, finops.CacheEntry{Hash: hash, CurrentCost: currentCost})
		return
	}

	fmt.Println("\033[1;33m\U0001f4a1 Suggested optimizations:\033[0m")
	for _, m := range mutations {
		fmt.Printf("   \u2022 [%s] %s: %s -> %s\n", m.ContainerName, m.FieldPath, m.OldValue, m.NewValue)
		fmt.Printf("     %s\n", m.Reasoning)
	}

	// Apply mutations to an in-memory copy of totals to recompute optimized cost.
	// Never trust AI-stated dollar figures; none are requested here anyway.
	optimizedTotals := totals
	for _, m := range mutations {
		applyMutationToTotals(&optimizedTotals, m)
	}
	optimizedCost := finops.MonthlyCost(optimizedTotals, region)
	delta := currentCost - optimizedCost
	if delta > 0 {
		fmt.Printf("\033[1;32m   Estimated optimized cost: $%.2f (saving $%.2f/mo)\033[0m\n", optimizedCost, delta)
	} else {
		fmt.Printf("\033[1;33m   Estimated cost with safer allocation: $%.2f (+$%.2f/mo for stability)\033[0m\n", optimizedCost, -delta)
	}

	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print("\n\U0001f449 Apply these cost-saving changes now? [y/N]: ")
		var input string
		fmt.Scanln(&input)
		if strings.ToLower(strings.TrimSpace(input)) == "y" {
			manifestPath := filepath.Join(cwd, "k8s", "base", "deployment.yml")
			for _, m := range mutations {
				if err := finops.ApplyMutation(manifestPath, m.ContainerName, m.FieldPath, m.NewValue); err != nil {
					fmt.Printf("\033[1;31m\u274c Could not apply mutation: %v\033[0m\n", err)
				}
			}
			fmt.Println("\033[1;32m\u2713\033[0m Applied. Re-run validate to confirm.")
		}
	}

	var cachedMutations []finops.CachedMutation
	for _, m := range mutations {
		cachedMutations = append(cachedMutations, finops.CachedMutation{
			ContainerName: m.ContainerName,
			FieldPath:     m.FieldPath,
			OldValue:      m.OldValue,
			NewValue:      m.NewValue,
			Reasoning:     m.Reasoning,
			ChangeType:    m.ChangeType,
		})
	}
	finops.SaveCache(cwd, finops.CacheEntry{Hash: hash, CurrentCost: currentCost, Mutations: cachedMutations})
}

// applyMutationToTotals updates an in-memory ResourceTotals based on one FinOpsMutation.
// Only requests fields affect cost (limits don't factor into MonthlyCost).
func applyMutationToTotals(totals *finops.ResourceTotals, m ai.FinOpsMutation) {
	switch m.FieldPath {
	case "requests.cpu":
		totals.CPUMillicores = totals.CPUMillicores - localParseCPU(m.OldValue) + localParseCPU(m.NewValue)
	case "requests.memory":
		totals.MemoryMiB = totals.MemoryMiB - localParseMemory(m.OldValue) + localParseMemory(m.NewValue)
	}
}

// localParseCPU mirrors finops.parseCPU (unexported) for cmd package use.
func localParseCPU(v string) int64 {
	if v == "" || v == "not set" {
		return 0
	}
	if strings.HasSuffix(v, "m") {
		var n int64
		fmt.Sscanf(strings.TrimSuffix(v, "m"), "%d", &n)
		return n
	}
	var f float64
	fmt.Sscanf(v, "%f", &f)
	return int64(f * 1000)
}

// localParseMemory mirrors finops.parseMemory (unexported) for cmd package use.
func localParseMemory(v string) int64 {
	if v == "" || v == "not set" {
		return 0
	}
	switch {
	case strings.HasSuffix(v, "Gi"):
		var f float64
		fmt.Sscanf(strings.TrimSuffix(v, "Gi"), "%f", &f)
		return int64(f * 1024)
	case strings.HasSuffix(v, "Mi"):
		var f float64
		fmt.Sscanf(strings.TrimSuffix(v, "Mi"), "%f", &f)
		return int64(f)
	}
	return 0
}
