package ports

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultRegistry     = 5001
	DefaultJenkins      = 8080
	DefaultJenkinsAgent = 50000
	DefaultTunnel       = 8081

	registryContainer = "local-registry"
	jenkinsContainer  = "local-jenkins"
)

// SandboxPorts holds host ports for local sandbox services.
type SandboxPorts struct {
	Registry     int `json:"registry"`
	Jenkins      int `json:"jenkins"`
	JenkinsAgent int `json:"jenkins_agent"`
	Tunnel       int `json:"tunnel"`
}

// Defaults returns the standard preferred port set.
func Defaults() SandboxPorts {
	return SandboxPorts{
		Registry:     DefaultRegistry,
		Jenkins:      DefaultJenkins,
		JenkinsAgent: DefaultJenkinsAgent,
		Tunnel:       DefaultTunnel,
	}
}

func (p SandboxPorts) RegistryHost() string {
	return fmt.Sprintf("127.0.0.1:%d", p.Registry)
}

func (p SandboxPorts) JenkinsURL() string {
	return fmt.Sprintf("http://localhost:%d", p.Jenkins)
}

func configPath(projectPath string) string {
	return filepath.Join(projectPath, ".pipeline", "ports.json")
}

// Load reads saved ports or returns defaults when no config exists yet.
func Load(projectPath string) (SandboxPorts, error) {
	data, err := os.ReadFile(configPath(projectPath))
	if err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		return SandboxPorts{}, err
	}

	var ports SandboxPorts
	if err := json.Unmarshal(data, &ports); err != nil {
		return SandboxPorts{}, err
	}
	return normalize(ports), nil
}

// Save persists the port allocation for a project.
func Save(projectPath string, ports SandboxPorts) error {
	dir := filepath.Join(projectPath, ".pipeline")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(normalize(ports), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(projectPath), data, 0644)
}

// Clear removes the saved port config (e.g. on destroy-ci).
func Clear(projectPath string) {
	os.Remove(configPath(projectPath))
}

// ResolveSandboxPorts picks ports for prep-ci: prefer defaults, fall back to free ports,
// and reuse ports from running sandbox containers when present.
func ResolveSandboxPorts(projectPath string, kindClusterExists bool) (SandboxPorts, error) {
	saved, _ := Load(projectPath)
	ports := Defaults()

	var err error

	ports.Registry, err = resolveServicePort(
		saved.Registry,
		DefaultRegistry,
		registryContainer,
		5000,
		kindClusterExists,
	)
	if err != nil {
		return SandboxPorts{}, fmt.Errorf("registry port: %w", err)
	}

	ports.Jenkins, err = resolveServicePort(
		saved.Jenkins,
		DefaultJenkins,
		jenkinsContainer,
		8080,
		false,
	)
	if err != nil {
		return SandboxPorts{}, fmt.Errorf("jenkins port: %w", err)
	}

	ports.JenkinsAgent, err = resolveServicePort(
		saved.JenkinsAgent,
		DefaultJenkinsAgent,
		"",
		0,
		false,
	)
	if err != nil {
		return SandboxPorts{}, fmt.Errorf("jenkins agent port: %w", err)
	}

	ports.Tunnel, err = resolveServicePort(
		saved.Tunnel,
		DefaultTunnel,
		"",
		0,
		false,
	)
	if err != nil {
		return SandboxPorts{}, fmt.Errorf("tunnel port: %w", err)
	}

	if err := Save(projectPath, ports); err != nil {
		return SandboxPorts{}, err
	}

	if err := SyncRegistryToProject(projectPath, ports.RegistryHost()); err != nil {
		fmt.Printf("\033[33m⚠️  Could not sync registry port to project files: %v\033[0m\n", err)
	}

	return ports, nil
}

// ResolveTunnelPort allocates a tunnel port at runtime (prefers default, then scans).
func ResolveTunnelPort(projectPath string) (int, error) {
	saved, _ := Load(projectPath)
	port, err := resolveServicePort(saved.Tunnel, DefaultTunnel, "", 0, false)
	if err != nil {
		return 0, err
	}

	saved.Tunnel = port
	_ = Save(projectPath, saved)
	return port, nil
}

func resolveServicePort(saved, preferred int, containerName string, internalPort int, lockToSaved bool) (int, error) {
	if containerName != "" && internalPort > 0 {
		if hostPort, ok := containerHostPort(containerName, internalPort); ok {
			if hostPort != preferred {
				fmt.Printf("\033[33mℹ️  Reusing %s on port %d\033[0m\n", containerName, hostPort)
			}
			return hostPort, nil
		}
	}

	if lockToSaved && saved > 0 {
		if isPortFree(saved) {
			return saved, nil
		}
		return 0, fmt.Errorf("port %d is required by the existing Kind cluster but is already in use — run 'pipeline destroy-ci' first", saved)
	}

	if isPortFree(preferred) {
		return preferred, nil
	}

	fmt.Printf("\033[33m⚠️  Port %d is busy — searching for a free port...\033[0m\n", preferred)

	for candidate := preferred + 1; candidate <= preferred+200; candidate++ {
		if isPortFree(candidate) {
			fmt.Printf("\033[1;32m✓\033[0m Using port %d instead of %d\n", candidate, preferred)
			return candidate, nil
		}
	}

	return 0, fmt.Errorf("no free port found near %d", preferred)
}

// AllocatePort returns the first free TCP port at or after start.
func AllocatePort(start int) (int, error) {
	for candidate := start; candidate <= start+200; candidate++ {
		if isPortFree(candidate) {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("no free port found near %d", start)
}

func isPortFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func containerHostPort(containerName string, internalPort int) (int, bool) {
	if !containerRunning(containerName) {
		return 0, false
	}

	out, err := exec.Command("docker", "port", containerName, fmt.Sprintf("%d/tcp", internalPort)).Output()
	if err != nil {
		return 0, false
	}

	line := strings.TrimSpace(string(out))
	if line == "" {
		return 0, false
	}

	// Format: "0.0.0.0:5001" or "[::]:5001"
	if idx := strings.LastIndex(line, ":"); idx != -1 {
		port, err := strconv.Atoi(line[idx+1:])
		if err == nil {
			return port, true
		}
	}
	return 0, false
}

func containerRunning(name string) bool {
	out, err := exec.Command("docker", "ps", "-q", "-f", fmt.Sprintf("name=%s", name)).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func normalize(p SandboxPorts) SandboxPorts {
	if p.Registry == 0 {
		p.Registry = DefaultRegistry
	}
	if p.Jenkins == 0 {
		p.Jenkins = DefaultJenkins
	}
	if p.JenkinsAgent == 0 {
		p.JenkinsAgent = DefaultJenkinsAgent
	}
	if p.Tunnel == 0 {
		p.Tunnel = DefaultTunnel
	}
	return p
}

func DefaultRegistryHost() string {
	return Defaults().RegistryHost()
}
