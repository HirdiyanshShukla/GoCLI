package preflight

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"
)

// RunSetupChecks executes all pre-flight validations
func RunSetupChecks() {
	fmt.Println("\n\033[1;36m✈️  Running Preflight Checks...\033[0m")

	// 0. Internet Check
	if err := CheckInternet(); err != nil {
		fmt.Printf("\033[1;31m❌ %s\033[0m\n", err.Error())
		os.Exit(1)
	}
	fmt.Println("\033[1;32m✓\033[0m Internet connection verified")

	// 1. Check System Dependencies
	if err := CheckDependencies(); err != nil {
		fmt.Printf("\033[1;31m❌ %s\033[0m\n", err.Error())
		os.Exit(1)
	}
	fmt.Println("\033[1;32m✓\033[0m Core dependencies found (docker, kind, kubectl)")

	// 2. Ensure Docker is awake
	if err := EnsureDockerRunning(); err != nil {
		fmt.Printf("\033[1;31m❌ %s\033[0m\n", err.Error())
		os.Exit(1)
	}
	fmt.Println("\033[1;32m✓\033[0m Docker daemon is active")

	// 3. Check default ports — warn if busy; prep-ci will allocate alternates automatically
	busy := []int{}
	for _, port := range []int{5001, 8080} {
		if err := checkPortAvailable(port); err != nil {
			busy = append(busy, port)
		}
	}
	if len(busy) > 0 {
		fmt.Printf("\033[33m⚠️  Default ports %v are in use — alternate ports will be allocated automatically\033[0m\n", busy)
	} else {
		fmt.Println("\033[1;32m✓\033[0m Default network ports (5001, 8080) are available")
	}
}

// CheckInternet ensures the user can reach GitHub and Docker Hub
func CheckInternet() error {
	client := http.Client{
		Timeout: 3 * time.Second,
	}

	_, err := client.Get("https://github.com")
	if err != nil {
		return fmt.Errorf("no internet connection detected. Pipeline requires internet to pull images and clone repositories")
	}

	return nil
}

// checkPortAvailable tries to bind to the port on all interfaces (matches Docker publish behavior).
func checkPortAvailable(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("Port %d is already in use by another program.\n%s", port, getKillCommandSuggestion(port))
	}
	ln.Close()
	return nil
}

// getKillCommandSuggestion provides OS-specific instructions to free the port
func getKillCommandSuggestion(port int) string {
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf(
			"\033[33m   👉 To free this port on Windows, run:\n      netstat -ano | findstr :%d\n      taskkill /PID <ProcessId> /F\033[0m",
			port,
		)
	case "darwin", "linux":
		return fmt.Sprintf(
			"\033[33m   👉 To free this port on Mac/Linux/WSL, run:\n      sudo lsof -i :%d\n      sudo kill -9 <PID>\033[0m",
			port,
		)
	default:
		return fmt.Sprintf(
			"\033[33m   👉 Please free port %d before continuing.\033[0m",
			port,
		)
	}
}