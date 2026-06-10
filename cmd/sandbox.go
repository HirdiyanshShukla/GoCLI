package cmd
 
import (
    "bufio"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
 
    "devsandbox/core"
    "devsandbox/core/ports"
    "devsandbox/core/preflight"
 
    "github.com/spf13/cobra"
)
 
var prepCiCmd = &cobra.Command{
    Use:   "prep-ci",
    Short: "Spins up an empty ephemeral cluster, registry, and Jenkins sandbox",
    Run: func(cmd *cobra.Command, args []string) {
 
        // 1. RUN PREFLIGHT
        preflight.RunSetupChecks()
 
        clusterName := "ephemeral-test"
 
        // CURRENT PROJECT DIRECTORY
        cwd, _ := os.Getwd()

        kindExists := kindClusterExists(clusterName)
        sandboxPorts, err := ports.ResolveSandboxPorts(cwd, kindExists)
        if err != nil {
            fmt.Printf("\033[1;31m❌ %s\033[0m\n", err.Error())
            return
        }

        fmt.Printf("\033[1;32m✓\033[0m Ports — registry: %s, jenkins: %d, tunnel: %d\n",
            sandboxPorts.RegistryHost(), sandboxPorts.Jenkins, sandboxPorts.Tunnel)

        // 2. REGISTRY
        if !isRegistryRunning() {
            fmt.Println("\n\033[33m⚠️ Local registry not running. Waking it up...\033[0m")

            removeStoppedContainer("local-registry")

            if err := core.ExecSilent("docker", "start", "local-registry"); err != nil {
                registryPort := bootRegistryContainer(sandboxPorts.Registry)
                if registryPort == 0 {
                    fmt.Println("\033[1;31m❌ Failed to start local registry.\033[0m")
                    return
                }
                sandboxPorts.Registry = registryPort
                p, _ := ports.Load(cwd)
                p.Registry = registryPort
                _ = ports.Save(cwd, p)
                _ = ports.SyncRegistryToProject(cwd, sandboxPorts.RegistryHost())
            }
        }
 
        fmt.Println("\n\033[1;36m🏗️ Building Kubernetes Sandbox & CI/CD Pipeline...\033[0m")
 
        // KIND CONFIG
        kindConfig := fmt.Sprintf(`
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."%s"]
    endpoint = ["http://local-registry:5000"]
`, sandboxPorts.RegistryHost())
 
        tempFile, _ := os.CreateTemp("", "kind-config-*.yaml")
        defer os.Remove(tempFile.Name())
 
        tempFile.WriteString(kindConfig)
        tempFile.Close()
 
        if !kindExists {
        core.ExecCommand(
            "Creating empty Kind cluster",
            false,
            true,
            "kind", "create", "cluster",
            "--name", clusterName,
            "--config", tempFile.Name(),
            "--image", "kindest/node:v1.30.0",
        )
        } else {
            fmt.Printf("\033[1;32m✓\033[0m Kind cluster '%s' already exists — reusing it\033[0m\n", clusterName)
        }
 
        core.ExecCommand(
            "Bridging Registry and Kind Networks",
            true,
            false,
            "docker", "network", "connect", "kind", "local-registry",
        )
 
        fmt.Println("\033[1;36m🔄 Generating isolated Kubeconfig for Jenkins...\033[0m")
        generateJenkinsKubeConfig(cwd)
 
        // JCasC
        cascYaml := `
jenkins:
  securityRealm:
    local:
      allowsSignup: false
      users:
       - id: "admin"
         password: "admin"
  authorizationStrategy:
    loggedInUsersCanDoAnything:
      allowAnonymousRead: false
`
 
        cascFile, _ := os.CreateTemp("", "casc-*.yaml")
        defer os.Remove(cascFile.Name())
 
        cascFile.WriteString(cascYaml)
        cascFile.Close()
 
        if !isJenkinsRunning() {

            fmt.Println("\033[1;36m🚀 Launching Jenkins Server (Automated Setup)...\033[0m")

            removeStoppedContainer(jenkinsName)

            if err := core.ExecSilent("docker", "start", jenkinsName); err != nil {
                var ok bool
                sandboxPorts, ok = bootAndProvisionJenkins(cwd, sandboxPorts, cascFile.Name())
                if !ok {
                    fmt.Println("\033[1;31m❌ Failed to start Jenkins. Free a port near 8080 or run 'destroy-ci' and retry.\033[0m")
                    return
                }
            }

		} else {
			fmt.Printf("\033[1;32m✅ Jenkins '%s' is active.\033[0m\n", jenkinsName)
		}

		fmt.Println("\n\033[1;32m✅ CI/CD Sandbox Infrastructure is LIVE!\033[0m")
		fmt.Printf("\033[33m👉 Jenkins UI: %s\033[0m\n", sandboxPorts.JenkinsURL())
		fmt.Printf("\033[33m👉 Docker Push API: %s\033[0m\n", sandboxPorts.RegistryHost())
		fmt.Println("\033[33m👉 Credentials: admin / admin\033[0m")

		// --- NEW HANDOFF MESSAGE ---
		cliName := filepath.Base(os.Args[0])
		fmt.Printf("\n\033[1;36m🚀 Ready to deploy? Run '%s run' to start your first build and track it live!\033[0m\n\n", cliName)
	},
}
var destroyCiCmd = &cobra.Command{
	Use:   "destroy-ci",
	Short: "Completely destroys the local CI/CD sandbox and optional scaffolding files",
	Run: func(cmd *cobra.Command, args []string) {

		clusterName := "ephemeral-test"

		fmt.Println("\033[1;31m💥 Commencing total teardown...\033[0m")

		core.ExecCommand(
			"Nuking containers",
			true,
			false,
			"docker", "rm", "-f",
			jenkinsName,
			"local-registry",
			"jenkins-sandbox",
		)

		core.ExecCommand(
			"Wiping persistent data",
			true,
			false,
			"docker", "volume", "rm",
			"local-jenkins-data",
			"local-registry-data",
		)

		core.ExecCommand(
			"Destroying Kind cluster",
			true,
			true,
			"kind", "delete", "cluster",
			"--name", clusterName,
		)

		// Clean up the temporary local kubeconfig
		cwd, _ := os.Getwd()
		os.Remove(filepath.Join(cwd, ".kubeconfig-jenkins"))
		ports.Clear(cwd)

		fmt.Println("\n\033[1;32m🧹 Infrastructure destroyed safely.\033[0m")

		// --- NEW LOGIC: Interactive Scaffolding Cleanup ---
		fmt.Println("\n\033[1;33m⚠️  Do you also want to delete the generated scaffolding files from this project?\033[0m")
		fmt.Println("  (This will remove Dockerfile, Jenkinsfile, pipeline.yaml, and the entire k8s/ directory)")
		fmt.Print("Proceed? (y/N): ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response == "y" || response == "yes" {
			cwd, err := os.Getwd()
			if err != nil {
				fmt.Printf("\033[1;31m❌ Could not get current directory: %v\033[0m\n", err)
				return
			}

			filesToRemove := []string{
				"Dockerfile",
				"Jenkinsfile",
				"pipeline.yaml",
				"k8s",
			}

			fmt.Println("\n\033[1;36m🧹 Cleaning up CI/CD files...\033[0m")
			deletedCount := 0

			for _, file := range filesToRemove {
				targetPath := filepath.Join(cwd, file)

				if _, err := os.Stat(targetPath); os.IsNotExist(err) {
					continue
				}

				err = os.RemoveAll(targetPath)
				if err != nil {
					fmt.Printf("\033[1;31m❌ Failed to delete %s: %v\033[0m\n", file, err)
				} else {
					fmt.Printf("\033[1;32m✓\033[0m Deleted %s\n", file)
					deletedCount++
				}
			}

			if deletedCount == 0 {
				fmt.Println("No scaffolding files found to delete.")
			} else {
				fmt.Println("\n\033[1;32m✨ Clean slate! All files and infrastructure destroyed.\033[0m\n")
			}
		} else {
			fmt.Println("\n\033[1;32m✨ Teardown complete! (Kept local scaffolding files)\033[0m\n")
		}
	},
}
 
func init() {
    rootCmd.AddCommand(prepCiCmd)
    rootCmd.AddCommand(destroyCiCmd)
}
 
const jenkinsName = "local-jenkins"
 
func isJenkinsRunning() bool {
    cmd := exec.Command("docker", "ps", "-q", "-f", fmt.Sprintf("name=%s", jenkinsName))
    output, err := cmd.Output()
 
    if err != nil {
        return false
    }
 
    return strings.TrimSpace(string(output)) != ""
}
 
func isRegistryRunning() bool {
    cmd := exec.Command("docker", "ps", "-q", "-f", "name=local-registry")
    output, err := cmd.Output()

    if err != nil {
        return false
    }

    return strings.TrimSpace(string(output)) != ""
}

func kindClusterExists(name string) bool {
	out, err := exec.Command("kind", "get", "clusters").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

func removeStoppedContainer(name string) {
	out, _ := exec.Command("docker", "ps", "-q", "-f", fmt.Sprintf("name=%s", name)).Output()
	if strings.TrimSpace(string(out)) != "" {
		return
	}
	_ = exec.Command("docker", "rm", "-f", name).Run()
}

func bootRegistryContainer(preferredPort int) int {
	port := preferredPort
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			next, err := ports.AllocatePort(port + 1)
			if err != nil {
				return 0
			}
			port = next
			removeStoppedContainer("local-registry")
		}

		fmt.Printf("\033[1;36m▶ Booting registry on host port %d...\033[0m\n", port)

		cmd := exec.Command(
			"docker", "run",
			"-d",
			"--restart=always",
			"-p", fmt.Sprintf("%d:5000", port),
			"--name", "local-registry",
			"-v", "local-registry-data:/var/lib/registry",
			"registry:2",
		)
		out, err := cmd.CombinedOutput()
		if err == nil {
			fmt.Printf("\033[32m✅ Registry started on port %d\033[0m\n", port)
			return port
		}

		output := string(out)
		if strings.Contains(output, "address already in use") || strings.Contains(output, "ports are not available") {
			fmt.Printf("\033[33m⚠️  Port %d unavailable at runtime — retrying...\033[0m\n", port)
			removeStoppedContainer("local-registry")
			continue
		}

		fmt.Printf("\033[1;31m❌ Registry boot failed:\033[0m\n%s", output)
		return 0
	}

	return 0
}

func bootJenkinsContainer(cwd, homeDir string, sandboxPorts ports.SandboxPorts) (int, bool) {
	port := sandboxPorts.Jenkins

	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			next, err := ports.AllocatePort(port + 1)
			if err != nil {
				return 0, false
			}
			port = next
			removeStoppedContainer(jenkinsName)
		}

		fmt.Printf("\033[1;36m▶ Booting Jenkins on host port %d...\033[0m\n", port)

		cmd := exec.Command(
			"docker", "run",
			"-d",
			"--restart=always",
			"-p", fmt.Sprintf("%d:8080", port),
			"-p", fmt.Sprintf("%d:50000", sandboxPorts.JenkinsAgent),
			"--name", jenkinsName,
			"-u", "root",
			"-e", fmt.Sprintf("HOST_HOME=%s", homeDir),
			"-e", fmt.Sprintf("HOST_PROJECT_PATH=%s", cwd),
			"-e", `JAVA_OPTS=-Djenkins.install.runSetupWizard=false`,
			"-e", "CASC_JENKINS_CONFIG=/var/jenkins_home/casc.yaml",
			"-v", "local-jenkins-data:/var/jenkins_home",
			"-v", "/var/run/docker.sock:/var/run/docker.sock",
			"-v", fmt.Sprintf("%s:%s", cwd, cwd),
			"jenkins/jenkins:lts",
		)
		out, err := cmd.CombinedOutput()
		if err == nil {
			fmt.Printf("\033[32m✅ Jenkins started on port %d\033[0m\n", port)
			return port, true
		}

		output := string(out)
		if strings.Contains(output, "address already in use") || strings.Contains(output, "ports are not available") {
			fmt.Printf("\033[33m⚠️  Port %d unavailable at runtime — retrying...\033[0m\n", port)
			removeStoppedContainer(jenkinsName)
			continue
		}

		fmt.Printf("\033[1;31m❌ Jenkins boot failed:\033[0m\n%s", output)
		return 0, false
	}

	return 0, false
}

func bootAndProvisionJenkins(cwd string, sandboxPorts ports.SandboxPorts, cascFilePath string) (ports.SandboxPorts, bool) {
	homeDir, _ := os.UserHomeDir()

	jenkinsPort, booted := bootJenkinsContainer(cwd, homeDir, sandboxPorts)
	if !booted {
		return sandboxPorts, false
	}
	sandboxPorts.Jenkins = jenkinsPort
	if p, err := ports.Load(cwd); err == nil {
		p.Jenkins = jenkinsPort
		_ = ports.Save(cwd, p)
	}

	if !isJenkinsRunning() {
		fmt.Println("\033[1;31m❌ Jenkins container failed to start.\033[0m")
		return sandboxPorts, false
	}

	core.ExecCommand(
		"Installing Docker CLI inside Jenkins (Takes ~2 min)",
		false,
		false,
		"docker", "exec",
		"-u", "root",
		jenkinsName,
		"bash", "-c",
		"apt-get update && apt-get install -y docker.io",
	)

	core.ExecCommand(
		"Installing Kustomize inside Jenkins",
		false,
		false,
		"docker", "exec",
		"-u", "root",
		jenkinsName,
		"bash", "-c",
		`ARCH=$(uname -m); if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; elif [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi && curl -sSL -O "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv5.4.2/kustomize_v5.4.2_linux_${ARCH}.tar.gz" && tar -xzf "kustomize_v5.4.2_linux_${ARCH}.tar.gz" && mv kustomize /usr/local/bin/ && rm "kustomize_v5.4.2_linux_${ARCH}.tar.gz"`,
	)

	core.ExecCommand(
		"Installing Jenkins Plugins (Takes ~2 min)",
		false,
		false,
		"docker", "exec",
		"-e", "JENKINS_UC_DOWNLOAD_TIMEOUT=60",
		"-e", "CURL_CONNECTION_TIMEOUT=60",
		"-e", "JENKINS_UC_DOWNLOAD=https://mirrors.tuna.tsinghua.edu.cn/jenkins",
		jenkinsName,
		"jenkins-plugin-cli",
		"--plugins",
		"git",
		"workflow-aggregator",
		"docker-workflow",
		"configuration-as-code",
		"ws-cleanup",
	)

	core.ExecCommand(
		"Injecting JCasC Configuration",
		true,
		false,
		"docker", "cp",
		cascFilePath,
		fmt.Sprintf("%s:/var/jenkins_home/casc.yaml", jenkinsName),
	)

	core.ExecCommand(
		"Applying plugins and configurations",
		false,
		true,
		"docker", "restart", jenkinsName,
	)

	fmt.Println("\033[33m⏳ Waiting for Jenkins to fully boot...\033[0m")

	core.ExecCommand(
		"Checking Jenkins API readiness",
		false,
		true,
		"docker", "exec",
		jenkinsName,
		"bash", "-c",
		`until curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/login | grep -q "200"; do sleep 3; done`,
	)

	return sandboxPorts, true
}

func generateJenkinsKubeConfig(projectPath string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("\033[31m❌ Could not find home directory to read kubeconfig\033[0m")
		return
	}

	kubeConfigPath := filepath.Join(homeDir, ".kube", "config")
	input, err := os.ReadFile(kubeConfigPath)
	if err != nil {
		fmt.Println("\033[31m❌ Could not read ~/.kube/config\033[0m")
		return
	}

	// Defensive check against empty files
	if len(input) == 0 {
		fmt.Println("\033[31m❌ ~/.kube/config is empty\033[0m")
		return
	}

	// Replace localhost with host.docker.internal for Jenkins
	output := strings.ReplaceAll(string(input), "127.0.0.1", "host.docker.internal")
	output = strings.ReplaceAll(output, "localhost", "host.docker.internal")

	localKubeConfigPath := filepath.Join(projectPath, ".kubeconfig-jenkins")
	err = os.WriteFile(localKubeConfigPath, []byte(output), 0644)
	if err != nil {
		fmt.Printf("\033[31m❌ Could not write isolated kubeconfig to %s: %v\033[0m\n", localKubeConfigPath, err)
		return
	}
}

