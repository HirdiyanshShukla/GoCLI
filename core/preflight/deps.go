package preflight

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CheckDependencies ensures required binaries are in the system PATH, and tries to install them if not.
func CheckDependencies() error {
	deps := []string{"kind", "kubectl"}

	for _, dep := range deps {
		_, err := exec.LookPath(dep)
		if err != nil {
			fmt.Printf("\033[33m⚠️ Missing dependency: %s. Attempting auto-install...\033[0m\n", dep)
			
			installErr := installDependency(dep)
			if installErr != nil {
				return fmt.Errorf("could not auto-install %s. Please install it manually (%v)", dep, installErr)
			}
			fmt.Printf("\033[1;32m✅ Successfully installed %s!\033[0m\n", dep)
		}
	}
	return nil
}

// installDependency uses OS-specific package managers to download missing tools
func installDependency(dep string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("brew", "install", dep).Run()
	case "windows":
		var winget *exec.Cmd
		if dep == "kind" {
			winget = exec.Command("winget", "install", "Kubernetes.kind", "--accept-source-agreements", "--accept-package-agreements")
		} else if dep == "kubectl" {
			winget = exec.Command("winget", "install", "Kubernetes.kubectl", "--accept-source-agreements", "--accept-package-agreements")
		} else {
			return fmt.Errorf("unsupported Windows dependency: %s", dep)
		}

		if err := winget.Run(); err != nil {
			return fmt.Errorf("winget install failed: %w", err)
		}
		refreshWindowsPath()
		if _, err := exec.LookPath(dep); err != nil {
			return fmt.Errorf(
				"%s was installed, but this terminal session can't see it yet.\n"+
					"   Close and reopen your terminal, then re-run this command", dep)
		}
		return nil
	case "linux":
		if dep == "kind" {
			return exec.Command("bash", "-c", "curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64 && chmod +x ./kind && sudo mv ./kind /usr/local/bin/kind").Run()
		}
		if dep == "kubectl" {
			return exec.Command("bash", "-c", `curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && chmod +x ./kubectl && sudo mv ./kubectl /usr/local/bin/kubectl`).Run()
		}
	}

	return fmt.Errorf("unsupported OS for auto-install")
}

// refreshWindowsPath adds known winget shim directories to THIS process's
// PATH. Winget's own PATH update only reaches new processes — without this,
// a tool installed moments ago is invisible to the rest of this run.
func refreshWindowsPath() {
	if runtime.GOOS != "windows" {
		return
	}
	candidates := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links"),
	}
	current := os.Getenv("PATH")
	for _, dir := range candidates {
		if !strings.Contains(current, dir) {
			current = dir + string(os.PathListSeparator) + current
		}
	}
	os.Setenv("PATH", current)
}