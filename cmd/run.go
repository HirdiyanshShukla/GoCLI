package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"opsai/core/ai"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Instantly syncs your Jenkinsfile, triggers a build, and tracks status live",
	Run: func(cmd *cobra.Command, args []string) {
		cliName := filepath.Base(os.Args[0])
		if !isJenkinsRunning() {
			fmt.Printf("\033[1;31m❌ Jenkins is not running. Start your sandbox first with '%s resume' or '%s prep-ci'\033[0m\n", cliName, cliName)
			return
		}

		cwd, _ := os.Getwd()
		rawName := filepath.Base(cwd)
		appName := strings.ToLower(rawName)
		appName = strings.ReplaceAll(appName, "_", "-")
		appName = strings.ReplaceAll(appName, " ", "-")

		fmt.Println("\033[1;36m🚀 Syncing pipeline and triggering build...\033[0m")

		jenkinsfileBytes, err := os.ReadFile(filepath.Join(cwd, "Jenkinsfile"))
		if err != nil {
			fmt.Printf("\033[1;31m❌ No Jenkinsfile found. Run '%s init' first.\033[0m\n", cliName)
			return
		}

		scriptContent := fmt.Sprintf("<![CDATA[%s]]>", string(jenkinsfileBytes))
		jobXML := fmt.Sprintf(`<?xml version='1.1' encoding='UTF-8'?>
<flow-definition plugin="workflow-job">
  <definition class="org.jenkinsci.plugins.workflow.cps.CpsFlowDefinition" plugin="workflow-cps">
    <script>%s</script>
    <sandbox>true</sandbox>
  </definition>
</flow-definition>`, scriptContent)

		xmlFile, _ := os.CreateTemp("", "job-*.xml")
		defer os.Remove(xmlFile.Name())
		xmlFile.WriteString(jobXML)
		xmlFile.Close()

		// 🔥 THE FIX: Smart Update & Queue Tracking Script
		apiScript := fmt.Sprintf(`#!/bin/bash
set -e
CRUMB=$(curl -s -c /tmp/cookies.txt -u admin:admin "http://localhost:8080/crumbIssuer/api/xml?xpath=concat(//crumbRequestField,\":\",//crumb)")

# 1. Try to cleanly update the existing job configuration
HTTP_STATUS=$(curl -s -o /dev/null -w "%%{http_code}" -X POST "http://localhost:8080/job/%s/config.xml" \
  -u admin:admin -b /tmp/cookies.txt -H "$CRUMB" \
  -H "Content-Type:text/xml" --data-binary @/tmp/job.xml)

# 2. If the job doesn't exist (HTTP 404), create it
if [ "$HTTP_STATUS" -eq 404 ]; then
  curl -s -X POST "http://localhost:8080/createItem?name=%s" \
    -u admin:admin -b /tmp/cookies.txt -H "$CRUMB" \
    -H "Content-Type:text/xml" --data-binary @/tmp/job.xml > /dev/null
fi

# 3. Fetch the expected upcoming build number
NEXT_BUILD=$(curl -s -u admin:admin -b /tmp/cookies.txt "http://localhost:8080/job/%s/api/json" | grep -o '"nextBuildNumber":[0-9]*' | cut -d':' -f2 || true)

# 4. Trigger the build
curl -s -X POST "http://localhost:8080/job/%s/build" \
  -u admin:admin -b /tmp/cookies.txt -H "$CRUMB" > /dev/null

# 5. Prevent Race Condition: Wait for the job to exit the queue
for i in {1..15}; do
  LAST_BUILD=$(curl -s -u admin:admin -b /tmp/cookies.txt "http://localhost:8080/job/%s/api/json" | grep -o '"lastBuild":{"_class":"[^"]*","number":[0-9]*' | grep -o '[0-9]*$' || true)
  if [ "$LAST_BUILD" == "$NEXT_BUILD" ]; then
    break
  fi
  sleep 1
done
`, appName, appName, appName, appName, appName)

		scriptFile, _ := os.CreateTemp("", "run-*.sh")
		defer os.Remove(scriptFile.Name())
		scriptFile.WriteString(apiScript)
		scriptFile.Close()

		if err := exec.Command("docker", "cp", xmlFile.Name(), "local-jenkins:/tmp/job.xml").Run(); err != nil {
			fmt.Println("\033[1;31m❌ Failed to inject job definition into Jenkins container.\033[0m")
			return
		}

		if err := exec.Command("docker", "cp", scriptFile.Name(), "local-jenkins:/tmp/run.sh").Run(); err != nil {
			fmt.Println("\033[1;31m❌ Failed to inject build script into Jenkins container.\033[0m")
			return
		}

		execCmd := exec.Command("docker", "exec", "local-jenkins", "bash", "/tmp/run.sh")
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		if err := execCmd.Run(); err != nil {
			fmt.Println("\033[1;31m❌ Build trigger failed. Check if Jenkins is healthy.\033[0m")
			return
		}

		monitorJenkinsBuild(appName)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}

// =====================================================================
// OBSERVABILITY & MONITORING LOGIC
// =====================================================================

type JenkinsBuildInfo struct {
	Building bool        `json:"building"`
	Result   interface{} `json:"result"`
	Number   int         `json:"number"`
}

func monitorJenkinsBuild(appName string) {
	fmt.Println("\n\033[1;36m⏳ Monitoring pipeline execution in real-time...\033[0m")

	time.Sleep(3 * time.Second)

	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://localhost:8080/job/%s/lastBuild/api/json", appName)
	consoleUrl := fmt.Sprintf("http://localhost:8080/job/%s/lastBuild/consoleText", appName)

	var lastBuildNum = -1
	var finished = false
	var dotCount = 0

	for !finished {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		req.SetBasicAuth("admin", "admin")

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			time.Sleep(3 * time.Second)
			continue
		}

		var info JenkinsBuildInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			resp.Body.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		resp.Body.Close()

		if lastBuildNum == -1 {
			lastBuildNum = info.Number
			fmt.Printf("🔄 Attached to Build #%d\n", lastBuildNum)
		}

		if info.Building {
			dotCount++
			dots := strings.Repeat(".", (dotCount%3)+1)
			fmt.Printf("\rPipeline is building%s   ", dots)
			time.Sleep(3 * time.Second)
		} else {
			finished = true
			fmt.Println("\n")
			resultStr, _ := info.Result.(string)
			if resultStr == "SUCCESS" {
				fmt.Printf("\033[1;32m✅ Pipeline Build #%d completed successfully!\033[0m\n", info.Number)
				monitorKubernetesDeployment(appName)
			} else {
				fmt.Printf("\033[1;31m❌ Pipeline Build #%d failed (Result: %s)\033[0m\n", info.Number, resultStr)
				fmt.Println("🔎 Fetching logs and invoking AI Log Analyzer...")

				logReq, err := http.NewRequest("GET", consoleUrl, nil)
				if err == nil {
					logReq.SetBasicAuth("admin", "admin")
					logResp, err := client.Do(logReq)
					if err == nil && logResp.StatusCode == 200 {
						defer logResp.Body.Close()
						body, err := io.ReadAll(logResp.Body)
						if err == nil && len(body) > 0 {
							printHighlightedLogSummary(string(body))

							fmt.Println("\033[1;36m🤖 Analyzing logs...\033[0m")
							analysis, err := ai.AnalyzeLogs(string(body))
							if err != nil {
								fmt.Printf("\033[1;31m❌ Log analysis failed: %v\033[0m\n", err)
							} else {
								ai.PrintAnalysis(analysis)
							}
						}
					}
				}
			}
		}
	}
}

type PodList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				Ready bool `json:"ready"`
				State struct {
					Waiting *struct {
						Reason  string `json:"reason"`
						Message string `json:"message"`
					} `json:"waiting"`
					Terminated *struct {
						Reason   string `json:"reason"`
						ExitCode int    `json:"exitCode"`
						Message  string `json:"message"`
					} `json:"terminated"`
				} `json:"state"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

func discoverNamespace(projectPath, appName string) string {
	for _, file := range []string{"namespace.yml", "namespace.yaml"} {
		nsPath := filepath.Join(projectPath, "k8s", "base", file)
		if data, err := os.ReadFile(nsPath); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.Contains(line, "name:") {
					parts := strings.Split(line, ":")
					if len(parts) > 1 {
						ns := strings.TrimSpace(parts[1])
						ns = strings.Trim(ns, `"'`)
						if ns != "" {
							return ns
						}
					}
				}
			}
		}
	}
	return appName + "-ns"
}

func monitorKubernetesDeployment(appName string) {
	fmt.Println("\033[1;36m⏳ Verifying Kubernetes deployment status...\033[0m")
	cwd, err := os.Getwd()
	var namespace string
	if err == nil {
		namespace = discoverNamespace(cwd, appName)
	} else {
		namespace = appName + "-ns"
	}

	var finished = false
	var dotCount = 0
	startTime := time.Now()
	timeout := 90 * time.Second

	for !finished {
		if time.Since(startTime) > timeout {
			fmt.Println("\n\033[1;31m❌ Timeout waiting for Kubernetes pods to start.\033[0m")
			os.Exit(1)
		}

		cmd := exec.Command("kubectl", "get", "pods", "-n", namespace, "-o", "json")
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("\r\033[33m⚠️ kubectl error: %s\033[0m\033[K", strings.TrimSpace(string(output)))
			time.Sleep(3 * time.Second)
			continue
		}

		var podList PodList
		if err := json.Unmarshal(output, &podList); err != nil {
			time.Sleep(3 * time.Second)
			continue
		}

		if len(podList.Items) == 0 {
			dotCount++
			dots := strings.Repeat(".", (dotCount%3)+1)
			fmt.Printf("\rWaiting for pods to be scheduled%s   ", dots)
			time.Sleep(3 * time.Second)
			continue
		}

		allRunningAndReady := true
		var failedPodName string
		var failureReason string
		var failureDetail string

		for _, pod := range podList.Items {
			podPhase := pod.Status.Phase

			if podPhase == "Failed" {
				allRunningAndReady = false
				failedPodName = pod.Metadata.Name
				failureReason = "PodPhaseFailed"
				failureDetail = "Pod failed to start."
				break
			}

			hasContainers := false
			for _, cs := range pod.Status.ContainerStatuses {
				hasContainers = true
				if !cs.Ready {
					allRunningAndReady = false

					if cs.State.Waiting != nil {
						reason := cs.State.Waiting.Reason
						if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "ErrImagePull" || reason == "CreateContainerConfigError" || reason == "CreateContainerError" || reason == "InvalidImageName" {
							failedPodName = pod.Metadata.Name
							failureReason = reason
							failureDetail = cs.State.Waiting.Message
							break
						}
					}

					if cs.State.Terminated != nil {
						if cs.State.Terminated.ExitCode != 0 {
							failedPodName = pod.Metadata.Name
							failureReason = "CrashLoopBackOff"
							failureDetail = fmt.Sprintf("Container terminated with exit code %d: %s", cs.State.Terminated.ExitCode, cs.State.Terminated.Message)
							break
						}
					}
				}
			}

			if !hasContainers && podPhase != "Running" {
				allRunningAndReady = false
			}

			if failedPodName != "" {
				break
			}
		}

		if failedPodName != "" {
			finished = true
			fmt.Println("\n")
			fmt.Printf("\033[1;31m❌ Kubernetes deployment failed! Pod '%s' entered failure state: %s\033[0m\n", failedPodName, failureReason)
			if failureDetail != "" {
				fmt.Printf("Details: %s\n", failureDetail)
			}
			fmt.Println("🔎 Fetching failing pod logs and invoking AI Log Analyzer...")

			logCmd := exec.Command("kubectl", "logs", failedPodName, "-n", namespace, "--tail=100")
			logOutput, logErr := logCmd.CombinedOutput()
			if logErr == nil && len(logOutput) > 0 {
				printHighlightedLogSummary(string(logOutput))

				fmt.Println("\033[1;36m🤖 Analyzing pod logs...\033[0m")
				analysis, err := ai.AnalyzeLogs(string(logOutput))
				if err != nil {
					fmt.Printf("\033[1;31m❌ Pod log analysis failed: %v\033[0m\n", err)
				} else {
					ai.PrintAnalysis(analysis)
				}
			} else {
				fmt.Println("⚠️  Could not retrieve pod logs (container may not have started yet).")
			}
			os.Exit(1)
		}

		if allRunningAndReady {
			finished = true
			fmt.Println("\n")
			fmt.Println("\033[1;32m✅ All Kubernetes pods are running and healthy!\033[0m")
		} else {
			dotCount++
			dots := strings.Repeat(".", (dotCount%3)+1)
			fmt.Printf("\rVerifying Kubernetes pods rollout status%s   ", dots)
			time.Sleep(3 * time.Second)
		}
	}
}