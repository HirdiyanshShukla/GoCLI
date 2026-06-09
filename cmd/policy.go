package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"devsandbox/core/config"
	"devsandbox/core/policy"
	"devsandbox/policies"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ── Parent command ─────────────────────────────────────────────────────────────

var policyRootCmd = &cobra.Command{
	Use:   "policy",
	Short: "Manage and run platform policy checks",
}

// ── pipeline policy list ───────────────────────────────────────────────────────

var policyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available policies and their current status",
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			fmt.Printf("\033[1;31m❌ %s\033[0m\n", err.Error())
			os.Exit(1)
		}

		disabled := make(map[string]bool)
		for _, name := range cfg.Policies.Disabled {
			disabled[name] = true
		}

		registry := policies.All()

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintf(w, "\n\033[1mNAME\tCATEGORY\tSEVERITY\tSTATUS\033[0m\n")
		fmt.Fprintf(w, "────────────────────\t────────────\t────────\t────────\n")
		for _, p := range registry {
			status := "\033[1;32menabled\033[0m"
			if disabled[p.Name()] {
				status = "\033[33mdisabled\033[0m"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name(), p.Category(), p.Severity(), status)
		}
		w.Flush()
		fmt.Println()
	},
}

// ── pipeline policy check ──────────────────────────────────────────────────────

var checkAllFlag bool
var checkPolicyFlag string

var policyCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Run policy checks against the current project",
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			fmt.Printf("\033[1;31m❌ %s\033[0m\n", err.Error())
			os.Exit(1)
		}

		registry := policies.All()

		// --all: run everything regardless of disabled list
		if checkAllFlag {
			cfg.Policies.Disabled = nil
		}

		// --policy <name>: run a single specific policy
		if checkPolicyFlag != "" {
			cliName := filepath.Base(os.Args[0])
			p, ok := registry[checkPolicyFlag]
			if !ok {
				fmt.Printf("\033[1;31m❌ Unknown policy: %s\033[0m\n", checkPolicyFlag)
				fmt.Printf("Run '%s policy list' to see available policies.\n", cliName)
				os.Exit(1)
			}
			result := p.Run(cwd, cfg.Policies.Config)
			result.PolicyName = p.Name()
			result.Severity = p.Severity()
			anyFailed := policy.PrintReport([]policy.PolicyResult{result})
			if anyFailed {
				os.Exit(1)
			}
			return
		}

		results := policy.RunPolicies(cwd, cfg, registry)
		anyFailed := policy.PrintReport(results)
		if anyFailed {
			os.Exit(1)
		}
	},
}

// ── pipeline policy enable ─────────────────────────────────────────────────────

var policyEnableCmd = &cobra.Command{
	Use:   "enable <policy-name>",
	Short: "Remove a policy from the disabled list in pipeline.yaml",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		cwd, _ := os.Getwd()

		cfg, err := loadPipelineConfig(cwd)
		if err != nil {
			return
		}

		// Remove name from disabled list
		newDisabled := []string{}
		wasDisabled := false
		for _, d := range cfg.Policies.Disabled {
			if d == name {
				wasDisabled = true
				continue
			}
			newDisabled = append(newDisabled, d)
		}
		cfg.Policies.Disabled = newDisabled

		if !wasDisabled {
			fmt.Printf("Policy '%s' was not in the disabled list — nothing to change.\n", name)
			return
		}

		if err := writePipelineConfig(cwd, cfg); err != nil {
			fmt.Printf("\033[1;31m❌ Failed to write pipeline.yaml: %v\033[0m\n", err)
			os.Exit(1)
		}
		fmt.Printf("\033[1;32m✓\033[0m Policy '%s' is now enabled.\n", name)
	},
}

// ── pipeline policy disable ────────────────────────────────────────────────────

var policyDisableCmd = &cobra.Command{
	Use:   "disable <policy-name>",
	Short: "Add a policy to the disabled list in pipeline.yaml",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		cwd, _ := os.Getwd()

		// Validate the policy name exists in the registry
		registry := policies.All()
		if _, ok := registry[name]; !ok {
			cliName := filepath.Base(os.Args[0])
			fmt.Printf("\033[1;31m❌ Unknown policy: %s\033[0m\n", name)
			fmt.Printf("Run '%s policy list' to see available policies.\n", cliName)
			os.Exit(1)
		}

		cfg, err := loadPipelineConfig(cwd)
		if err != nil {
			return
		}

		// Idempotent: only add if not already disabled
		for _, d := range cfg.Policies.Disabled {
			if d == name {
				fmt.Printf("Policy '%s' is already disabled.\n", name)
				return
			}
		}

		cfg.Policies.Disabled = append(cfg.Policies.Disabled, name)

		if err := writePipelineConfig(cwd, cfg); err != nil {
			fmt.Printf("\033[1;31m❌ Failed to write pipeline.yaml: %v\033[0m\n", err)
			os.Exit(1)
		}
		fmt.Printf("\033[1;32m✓\033[0m Policy '%s' is now disabled.\n", name)
	},
}

// ── pipeline policy explain ────────────────────────────────────────────────────

var policyExplainCmd = &cobra.Command{
	Use:   "explain <policy-name>",
	Short: "Show detailed information about a specific policy",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		registry := policies.All()
		p, ok := registry[name]
		if !ok {
			cliName := filepath.Base(os.Args[0])
			fmt.Printf("\033[1;31m❌ Unknown policy: %s\033[0m\n", name)
			fmt.Printf("Run '%s policy list' to see available policies.\n", cliName)
			os.Exit(1)
		}

		fmt.Printf("\n\033[1m%s\033[0m\n", p.DisplayName())
		fmt.Printf("  Category  : %s\n", p.Category())
		fmt.Printf("  Severity  : %s\n", p.Severity())
		fmt.Printf("  Description:\n    %s\n", p.Description())
		cliName2 := filepath.Base(os.Args[0])
		fmt.Printf("\nTo disable: %s policy disable %s\n\n", cliName2, p.Name())
	},
}

// ── Helpers ────────────────────────────────────────────────────────────────────

// loadPipelineConfig loads pipeline.yaml; prints instructions and returns an
// error if the file does not exist so callers know to abort.
func loadPipelineConfig(dir string) (config.PipelineConfig, error) {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		fmt.Printf("\033[1;31m❌ %s\033[0m\n", err.Error())
		return cfg, err
	}

	// Check if file exists at all; LoadConfig returns empty struct when missing.
	if _, statErr := os.Stat(filepath.Join(dir, "pipeline.yaml")); os.IsNotExist(statErr) {
		cliName := filepath.Base(os.Args[0])
		fmt.Printf("No pipeline.yaml found. Run '%s init' first to create one.\n", cliName)
		return cfg, fmt.Errorf("pipeline.yaml not found")
	}

	return cfg, nil
}

// writePipelineConfig marshals cfg back to pipeline.yaml.
// Note: YAML comments are not preserved by yaml.Marshal.
func writePipelineConfig(dir string, cfg config.PipelineConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "pipeline.yaml"), data, 0644)
}

// ── Registration ───────────────────────────────────────────────────────────────

func init() {
	// Flags
	policyCheckCmd.Flags().BoolVar(&checkAllFlag, "all", false, "Run all policies, ignoring the disabled list")
	policyCheckCmd.Flags().StringVar(&checkPolicyFlag, "policy", "", "Run a single policy by name")

	// Sub-commands
	policyRootCmd.AddCommand(policyListCmd)
	policyRootCmd.AddCommand(policyCheckCmd)
	policyRootCmd.AddCommand(policyEnableCmd)
	policyRootCmd.AddCommand(policyDisableCmd)
	policyRootCmd.AddCommand(policyExplainCmd)

	// Register under root
	rootCmd.AddCommand(policyRootCmd)
}
