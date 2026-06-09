package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devsandbox/core"
	"devsandbox/core/ai"
	"devsandbox/scaffolding_engine/core/detector"
	"devsandbox/scaffolding_engine/core/generator"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initializes scaffolding for the detected framework",
	Long:  `Detects the project framework in the current directory and generates scaffolding files and a pipeline.yaml starter.`,
	Run: func(cmd *cobra.Command, args []string) {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Println("Error getting current directory:", err)
			return
		}

		fmt.Println("\033[1;36m🤖 Analyzing project structure and inferring configuration...\033[0m")

		aiResult, err := detector.AIDetectFramework(cwd)
		if err != nil {
			fmt.Printf("\033[33m⚠️  AI detection failed: %s\033[0m\n", err.Error())
			if os.Getenv("GEMINI_API_KEY") != "" {
				fmt.Println("\n\033[1;35m🤖 Auto-analyzing framework detection failure...\033[0m")
				analysis, analysisErr := ai.AnalyzeLogs(fmt.Sprintf("Init Stage: AI Framework Detection failed.\nError: %v", err))
				if analysisErr == nil {
					ai.PrintAnalysis(analysis)
					core.AskAndApplyFixes(analysis)
				}
			}
			os.Exit(1)
		}

		// The Generator handles the routing validation dynamically
		err = generator.GenerateFiles(aiResult.Framework, cwd, aiResult.EntryPath, &aiResult)
		if err != nil {
			fmt.Printf("\033[1;31m❌ %s\033[0m\n", err.Error())
			if os.Getenv("GEMINI_API_KEY") != "" {
				fmt.Println("\n\033[1;35m🤖 Auto-analyzing scaffolding generation failure...\033[0m")
				analysis, analysisErr := ai.AnalyzeLogs(fmt.Sprintf("Init Stage: Generating scaffolding files failed for framework '%s'.\nError: %v", aiResult.Framework, err))
				if analysisErr == nil {
					ai.PrintAnalysis(analysis)
					core.AskAndApplyFixes(analysis)
				}
			}
			os.Exit(1)
		}

		fmt.Println("\n\033[1;32m✨ Scaffolding generation successful!\033[0m")

		yamlPath := filepath.Join(cwd, "pipeline.yaml")
		if _, statErr := os.Stat(yamlPath); os.IsNotExist(statErr) {
			yamlContent := buildPipelineYaml(aiResult.Framework)
			if writeErr := os.WriteFile(yamlPath, []byte(yamlContent), 0644); writeErr != nil {
				fmt.Printf("\033[33m⚠️  Could not write pipeline.yaml: %v\033[0m\n", writeErr)
			} else {
				fmt.Println("\033[1;32m✓\033[0m Generated starter pipeline.yaml")
			}
		} else {
			fmt.Println("⚠️  pipeline.yaml already exists — skipping (your customizations are preserved)")
		}

		fmt.Println("\n\033[1;33m❓ Do you want to boot the local CI/CD sandbox for this project?\033[0m")
		fmt.Print("This takes ~5 minutes. (y/N): ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response == "y" || response == "yes" {
			fmt.Println("\n\033[1;36mTransitioning to prep-ci...\033[0m")
			prepCiCmd.Run(cmd, args)
		} else {
			cliName := filepath.Base(os.Args[0])
			fmt.Printf("\n\033[1;32m✅ Setup complete. Run '%s prep-ci' whenever you are ready to test locally.\033[0m\n", cliName)
		}
	},
}

func buildPipelineYaml(framework string) string {
	// Shared header
	header := `# pipeline.yaml
# Platform configuration file — commit this to version control.
# All fields are optional. Uncomment and set values to override platform defaults.

version: "1"

app:
  # name: my-service              # Override the app name (default: derived from folder name)
  # port: `

	// Framework-specific config hints
	var portHint, langBlock, healthHint, testHint string

	switch framework {
	case "django", "fastapi":
		portHint = "8000                    # Override the container port (default: 8000)"
		langBlock = `  # python_version: "3.12"        # Override Python version (default: 3.12)`
		healthHint = `  # health_path: "/health"           # Override health check path`
		testHint = `  # test_command: "pytest tests/"     # Override test command`

	case "expressjs", "react":
		if framework == "expressjs" {
			portHint = "3000                    # Override the container port (default: 3000)"
		} else {
			portHint = "8080                    # Override the container port (default: 8080)"
		}

		langBlock = `  # node_version: "22"            # Override Node.js version (default: 22)`
		healthHint = `  # health_path: "/health"           # Override health check path`
		testHint = `  # test_command: "npm test"           # Override test command`

	case "java_springboot":
		portHint = "8080                    # Override the container port (default: 8080)"
		langBlock = `  # java_version: "17"            # Override Java version (default: 17)`
		healthHint = `  # health_path: "/actuator/health"  # Override health check path`
		testHint = `  # test_command: "./mvnw test"        # Override test command`

	default:
		portHint = "8080                    # Override the container port"
		langBlock = ""
		healthHint = `  # health_path: "/health"           # Override health check path`
		testHint = `  # test_command: "your-test-command"  # Override test command`
	}

	// Shared footer
	footer := `
` + healthHint + `
` + testHint + `

# env:
#   - name: ENVIRONMENT
#     value: "production"
#   - name: FEATURE_FLAGS_URL
#     value: "https://flags.internal.yourcompany.com"

# secrets:
#   - name: DATABASE_URL
#     secret_name: my-db-secret
#     secret_key: connection-string

policies:
  mode: opt-out
  # disabled:
  #   - api-versioning
  # config:
  #   feature-flags:
  #     library: "your-flag-library"
`

	return header + portHint + "\n" + langBlock + footer
}

func init() {
	rootCmd.AddCommand(initCmd)
}