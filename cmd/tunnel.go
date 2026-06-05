package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

var tunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Opens a secure port-forward tunnel to your deployed application",
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		rawName := filepath.Base(cwd)
		appName := strings.ToLower(rawName)
		appName = strings.ReplaceAll(appName, "_", "-")
		appName = strings.ReplaceAll(appName, " ", "-")
		re := regexp.MustCompile(`[^a-z0-9-]`)
		appName = re.ReplaceAllString(appName, "")
		appName = strings.Trim(appName, "-")

		namespace := appName + "-ns"


		fmt.Printf("\033[1;36m🌍 Opening a direct tunnel to '%s'...\033[0m\n", appName)
		fmt.Println("\033[1;32m👉 App will be live at: http://localhost:8081\033[0m")
		fmt.Println("\033[33mPress [Ctrl+C] to close the tunnel when you are done.\n\033[0m")

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigChan
			fmt.Println("\n\033[1;36m🚪 Port-forwarding stopped.\033[0m")
			os.Exit(0)
		}()

		// NATIVE OS EXECUTION (No Bash!)
		c := exec.Command("kubectl", "port-forward", fmt.Sprintf("svc/%s", appName), "8081:80", "-n", namespace)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		
		err := c.Run()
		if err != nil {
			fmt.Println("\n\033[31m❌ Tunnel disconnected or failed to start. Is the app fully deployed and '1/1 Ready'?\033[0m")
		}
	},
}

func init() {
	rootCmd.AddCommand(tunnelCmd)
}