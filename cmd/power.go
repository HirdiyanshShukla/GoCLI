package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"devsandbox/core"

	"github.com/spf13/cobra"
)

var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Suspends the CI/CD sandbox to save RAM and battery — data is preserved",
	Run: func(cmd *cobra.Command, args []string) {
		cliName := filepath.Base(os.Args[0])
		fmt.Println("\033[1;33m⏸️  Pausing infrastructure...\033[0m")

		// Stop in reverse dependency order: Jenkins first, then cluster, then registry
		core.ExecCommand("Stopping Jenkins", true, true,
			"docker", "stop", "local-jenkins")
		core.ExecCommand("Stopping Kind cluster node", true, true,
			"docker", "stop", "ephemeral-test-control-plane")
		core.ExecCommand("Stopping registry", true, true,
			"docker", "stop", "local-registry")

		fmt.Println("\n\033[1;32m✅ Sandbox paused. All data preserved.\033[0m")
		fmt.Printf("\033[33m👉 Resume with: %s resume\033[0m\n", cliName)
	},
}

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Wakes up the paused CI/CD sandbox",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("\033[1;36m▶️  Waking up infrastructure...\033[0m")

		// Start in dependency order: registry first, then cluster, then Jenkins
		core.ExecCommand("Starting registry", false, true,
			"docker", "start", "local-registry")
		core.ExecCommand("Starting Kind cluster node", false, true,
			"docker", "start", "ephemeral-test-control-plane")

		// Wait for the Kind node to be Ready before starting Jenkins
		// Jenkins connects to the cluster on startup — if cluster isn't ready it fails silently
		fmt.Println("\033[33m⏳ Waiting for Kubernetes node to become ready...\033[0m")
		waitForKindNode()

		core.ExecCommand("Starting Jenkins", false, true,
			"docker", "start", "local-jenkins")

		sandboxPorts := loadSandboxPorts()
		fmt.Println("\n\033[1;32m✅ Sandbox is back online.\033[0m")
		fmt.Printf("\033[33m👉 Jenkins UI: %s\033[0m\n", sandboxPorts.JenkinsURL())
		fmt.Println("\033[33m👉 Credentials: admin / admin\033[0m")
	},
}

// waitForKindNode polls kubectl until the node reports Ready status.
// Times out after 60 seconds and continues anyway — Jenkins will retry.
func waitForKindNode() {
	for i := 0; i < 30; i++ {
		out, err := exec.Command(
			"kubectl", "get", "nodes",
			"--context", "kind-ephemeral-test",
			"--no-headers",
		).Output()
		if err == nil && strings.Contains(string(out), "Ready") {
			fmt.Println("\033[1;32m✓\033[0m Kubernetes node is Ready")
			return
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Println("\033[33m⚠️  Node readiness check timed out — continuing anyway.\033[0m")
}

func init() {
	rootCmd.AddCommand(pauseCmd)
	rootCmd.AddCommand(resumeCmd)
}
