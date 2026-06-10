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

	"devsandbox/core"
	"devsandbox/core/ai"
	"devsandbox/core/ports"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{

	Use: "run",

	Short: "Instantly syncs your Jenkinsfile, triggers a build, and tracks status live",

	Run: func(cmd *cobra.Command, args []string) {

		cwd, _ := os.Getwd()

		// Guardrail -- must run from root

		core.RequireProjectRoot(cwd)

		cliName := filepath.Base(os.Args[0])

		if !isJenkinsRunning() {

			fmt.Printf("\033[1;31m❌ Jenkins is not running. Start your sandbox first with '%s resume' or '%s prep-ci'\033[0m\n", cliName, cliName)

			return

		}

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

		// Wrap in pipeline job XML

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

		// Jenkins API script

		apiScript := fmt.Sprintf(`#!/bin/bash

set -e

APP_NAME="%s"

CRUMB=$(curl -s -c /tmp/cookies.txt -u admin:admin "http://localhost:8080/crumbIssuer/api/xml?xpath=concat(//crumbRequestField,\":\",//crumb)")
 
STATUS=$(curl -s -o /dev/null -w "%%{http_code}" -u admin:admin -b /tmp/cookies.txt "http://localhost:8080/job/${APP_NAME}/api/json")
 
if [ "$STATUS" -eq 404 ]; then
  # Create new job
  curl -s -X POST "http://localhost:8080/createItem?name=${APP_NAME}" \
    -u admin:admin -b /tmp/cookies.txt -H "$CRUMB" \
    -H "Content-Type:text/xml" --data-binary @/tmp/job.xml > /dev/null
else
  # Update existing job
  curl -s -X POST "http://localhost:8080/job/${APP_NAME}/config.xml" \
    -u admin:admin -b /tmp/cookies.txt -H "$CRUMB" \
    -H "Content-Type:text/xml" --data-binary @/tmp/job.xml > /dev/null
fi
 
NEXT_BUILD=$(curl -s -u admin:admin -b /tmp/cookies.txt "http://localhost:8080/job/${APP_NAME}/api/json" | grep -o '"nextBuildNumber":[0-9]*' | cut -d':' -f2 || true)
 
curl -s -X POST "http://localhost:8080/job/${APP_NAME}/build" \
  -u admin:admin -b /tmp/cookies.txt -H "$CRUMB" > /dev/null
 
for i in {1..15}; do
  LAST_BUILD=$(curl -s -u admin:admin -b /tmp/cookies.txt "http://localhost:8080/job/${APP_NAME}/api/json" | grep -o '"lastBuild":{"_class":"[^"]*","number":[0-9]*' | grep -o '[0-9]*$' || true)
  if [ "$LAST_BUILD" == "$NEXT_BUILD" ]; then
    break
  fi
  sleep 1
done

`, appName)

		scriptFile, _ := os.CreateTemp("", "run-*.sh")

		defer os.Remove(scriptFile.Name())

		scriptFile.WriteString(apiScript)

		scriptFile.Close()

		if err := exec.Command("docker", "cp", xmlFile.Name(), "local-jenkins:/tmp/job.xml").Run(); err != nil {

			fmt.Println("\033[1;31m❌ Failed to inject job definition into Jenkins.\033[0m")

			return

		}

		if err := exec.Command("docker", "cp", scriptFile.Name(), "local-jenkins:/tmp/run.sh").Run(); err != nil {

			fmt.Println("\033[1;31m❌ Failed to inject build script into Jenkins.\033[0m")

			return

		}

		execCmd := exec.Command("docker", "exec", "local-jenkins", "bash", "/tmp/run.sh")

		if err := execCmd.Run(); err != nil {

			fmt.Println("\033[1;31m❌ Build trigger failed. Check Jenkins is healthy.\033[0m")

			return

		}

		sandboxPorts := loadSandboxPorts()
		fmt.Printf("\n\033[1;32m✅ Build triggered!\033[0m\n")
		fmt.Printf("\033[33m👉 Track it live at: %s/job/%s\033[0m\n", sandboxPorts.JenkinsURL(), appName)

		// Wait for Jenkins to finish BEFORE checking Kubernetes

		waitForJenkinsBuild(appName, sandboxPorts)

	},
}

func init() {

	rootCmd.AddCommand(runCmd)

}

func waitForJenkinsBuild(appName string, sandboxPorts ports.SandboxPorts) {
	fmt.Printf("\n\033[1;36m⏳ Waiting for Jenkins CI pipeline to start...\033[0m\n")

	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("%s/job/%s/lastBuild/api/json", sandboxPorts.JenkinsURL(), appName)

	buildAnnounced := false

	for {
		time.Sleep(3 * time.Second)

		req, _ := http.NewRequest("GET", url, nil)
		req.SetBasicAuth("admin", "admin")

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var buildStatus struct {
			Building bool   `json:"building"`
			Result   string `json:"result"`
			Number   int    `json:"number"`
		}
		json.Unmarshal(body, &buildStatus)

		if buildStatus.Number > 0 && !buildAnnounced {
			fmt.Printf("\033[1;34m▶ Jenkins CI Build #%d is running \033[0m", buildStatus.Number)
			buildAnnounced = true
		}

		if buildStatus.Building {
			if buildAnnounced {
				fmt.Print("\033[33m.\033[0m")
			}
			continue
		}

		if buildStatus.Result == "SUCCESS" {
			fmt.Printf("\n\033[1;32m✅ Jenkins CI Build #%d succeeded!\033[0m\n", buildStatus.Number)
			monitorKubernetesDeployment(appName)
			return
		} else {
			fmt.Printf("\n\033[1;31m❌ Jenkins CI Build #%d failed (Result: %s)\033[0m\n", buildStatus.Number, buildStatus.Result)
			consoleUrl := fmt.Sprintf("%s/job/%s/lastBuild/consoleText", sandboxPorts.JenkinsURL(), appName)
			logReq, err := http.NewRequest("GET", consoleUrl, nil)
			if err == nil {
				logReq.SetBasicAuth("admin", "admin")
				logResp, err := client.Do(logReq)
				if err == nil && logResp.StatusCode == 200 {
					defer logResp.Body.Close()
					logBody, err := io.ReadAll(logResp.Body)
					if err == nil && len(logBody) > 0 {
						fmt.Println("\n\033[1;35m🤖 Pipeline failed. Auto-analyzing console logs with Gemini AI...\033[0m")
						analysis, err := ai.AnalyzeLogs(string(logBody))
						if err != nil {
							fmt.Printf("\033[1;31m❌ Log analysis failed: %v\033[0m\n", err)
						} else {
							ai.PrintAnalysis(analysis)
							core.AskAndApplyFixes(analysis)
						}
					}
				}
			}
			os.Exit(1)
		}
	}
}

func monitorKubernetesDeployment(appName string) {

	namespace := appName + "-ns"

	maxAttempts := 60

	type ContainerState struct {
		Waiting *struct {
			Reason string `json:"reason"`

			Message string `json:"message"`
		} `json:"waiting"`

		Running *struct{} `json:"running"`

		Terminated *struct {
			Reason string `json:"reason"`
		} `json:"terminated"`
	}

	type ContainerStatus struct {
		Name string `json:"name"`

		Ready bool `json:"ready"`

		State ContainerState `json:"state"`
	}

	type PodStatus struct {
		Phase string `json:"phase"`

		ContainerStatuses []ContainerStatus `json:"containerStatuses"`
	}

	type PodMetadata struct {
		Name string `json:"name"`
	}

	type Pod struct {
		Metadata PodMetadata `json:"metadata"`

		Status PodStatus `json:"status"`
	}

	type PodList struct {
		Items []Pod `json:"items"`
	}

	var (
		failureReason string

		failureDetail string

		failedPodName string

		finished bool
	)

	fmt.Printf("\n\033[1;36m⏳ Monitoring deployment of '%s' in namespace '%s'...\033[0m\n", appName, namespace)

	for attempt := 0; attempt < maxAttempts && !finished; attempt++ {

		time.Sleep(5 * time.Second)

		out, err := exec.Command("kubectl", "get", "pods", "-n", namespace, "-o", "json").Output()

		if err != nil {

			fmt.Printf("\033[33m⏳ Waiting for pods to appear... (%ds)\033[0m\r", (attempt+1)*5)

			continue

		}

		var podList PodList

		if jsonErr := json.Unmarshal(out, &podList); jsonErr != nil || len(podList.Items) == 0 {

			fmt.Printf("\033[33m⏳ No pods found yet... (%ds)\033[0m\r", (attempt+1)*5)

			continue

		}

		allRunningAndReady := true

		failureReason = ""

		failureDetail = ""

		failedPodName = ""

		hasContainers := false

		podPhase := ""

		for _, pod := range podList.Items {

			podPhase = pod.Status.Phase

			if len(pod.Status.ContainerStatuses) > 0 {

				hasContainers = true

				for _, cs := range pod.Status.ContainerStatuses {

					if !cs.Ready {

						allRunningAndReady = false

					}

					// THE CATCH-ALL LOGIC

					if cs.State.Waiting != nil {

						reason := cs.State.Waiting.Reason

						if reason != "" && reason != "ContainerCreating" && reason != "PodInitializing" {

							failedPodName = pod.Metadata.Name

							failureReason = reason

							failureDetail = cs.State.Waiting.Message

							break

						}

					}

					if cs.State.Terminated != nil {

						if cs.State.Terminated.Reason == "Error" || cs.State.Terminated.Reason == "OOMKilled" {

							failedPodName = pod.Metadata.Name

							failureReason = cs.State.Terminated.Reason

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

				fmt.Println("\033[1;36m🤖 Analyzing pod logs...\033[0m")

				analysis, err := ai.AnalyzeLogs(string(logOutput))

				if err != nil {

					fmt.Printf("\033[1;31m❌ Pod log analysis failed: %v\033[0m\n", err)

				} else {

					ai.PrintAnalysis(analysis)
					core.AskAndApplyFixes(analysis)

				}

			} else {

				fmt.Println("⚠️  Could not retrieve pod logs (container may not have started yet).")

				cliName := filepath.Base(os.Args[0])
				fmt.Printf("\033[33m👉 Run '%s logs analyze' to diagnose further.\033[0m\n", cliName)

			}

			return

		}

		if allRunningAndReady {

			finished = true

			fmt.Println("\n")

			fmt.Println("\033[1;32m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")

			fmt.Println("\033[1;32m  ✅ Project Deployed Successfully!\033[0m")

			fmt.Printf("\033[1;32m  App: %s is live and healthy\033[0m\n", appName)

			fmt.Println("\033[1;32m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")

			cliName := filepath.Base(os.Args[0])
			fmt.Printf("\033[33m👉 Run '%s tunnel' to access it at http://localhost:8081\033[0m\n\n", cliName)

		} else {

			fmt.Printf("\033[33m⏳ Pods initializing... (%ds elapsed)\033[0m\r", (attempt+1)*5)

		}

	}

	if !finished {

		fmt.Println("\n\033[1;31m❌ Deployment timed out after 5 minutes.\033[0m")

		cliName2 := filepath.Base(os.Args[0])
		fmt.Printf("\033[33m👉 Run '%s logs analyze' to diagnose the issue.\033[0m\n", cliName2)

	}

}
