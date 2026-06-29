package finops

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ContainerResources holds the parsed resource fields for one container.
// Fields are empty string when not set in the manifest. This distinction
// matters for the AI mutation step (modifying vs adding a field).
type ContainerResources struct {
	Name           string
	RequestsCPU    string
	RequestsMemory string
	LimitsCPU      string
	LimitsMemory   string
}

// RenderProdManifest runs kustomize against the prod overlay and returns the
// rendered YAML. Returns an error if kustomize is unavailable or the overlay
// doesn't exist; callers should treat this as "skip the stage", not fatal.
func RenderProdManifest(projectPath string) (string, error) {
	cmd := exec.Command("kubectl", "kustomize", filepath.Join(projectPath, "k8s", "overlays", "prod"))
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("could not render prod overlay: %w", err)
	}
	return string(out), nil
}

// ParseContainerResources walks every Deployment document in the rendered
// manifest and extracts resources.requests/limits for every container,
// including sidecars and init containers. Also returns the replica count
// from the first Deployment found.
func ParseContainerResources(manifestYAML string) ([]ContainerResources, int, error) {
	decoder := yaml.NewDecoder(strings.NewReader(manifestYAML))
	var containers []ContainerResources
	replicas := 1

	for {
		var doc map[string]interface{}
		if err := decoder.Decode(&doc); err != nil {
			break
		}
		kind, _ := doc["kind"].(string)
		if kind != "Deployment" {
			continue
		}

		spec, _ := doc["spec"].(map[string]interface{})
		if r, ok := spec["replicas"].(int); ok {
			replicas = r
		}

		template, _ := spec["template"].(map[string]interface{})
		podSpec, _ := template["spec"].(map[string]interface{})

		for _, key := range []string{"containers", "initContainers"} {
			list, _ := podSpec[key].([]interface{})
			for _, c := range list {
				cMap, _ := c.(map[string]interface{})
				name, _ := cMap["name"].(string)
				res, _ := cMap["resources"].(map[string]interface{})
				cr := ContainerResources{Name: name}

				if reqs, ok := res["requests"].(map[string]interface{}); ok {
					if v, ok := reqs["cpu"].(string); ok {
						cr.RequestsCPU = v
					}
					if v, ok := reqs["memory"].(string); ok {
						cr.RequestsMemory = v
					}
				}
				if lims, ok := res["limits"].(map[string]interface{}); ok {
					if v, ok := lims["cpu"].(string); ok {
						cr.LimitsCPU = v
					}
					if v, ok := lims["memory"].(string); ok {
						cr.LimitsMemory = v
					}
				}
				containers = append(containers, cr)
			}
		}
	}
	return containers, replicas, nil
}

// SumResources converts all containers requests into millicores/MiB and
// sums them, then multiplies by replicas. Falls back to 0 for any unset field.
func SumResources(containers []ContainerResources, replicas int) ResourceTotals {
	var totals ResourceTotals
	totals.Replicas = replicas
	for _, c := range containers {
		totals.CPUMillicores += parseCPU(c.RequestsCPU)
		totals.MemoryMiB += parseMemory(c.RequestsMemory)
	}
	return totals
}

func parseCPU(v string) int64 {
	if v == "" {
		return 0
	}
	if strings.HasSuffix(v, "m") {
		n, _ := strconv.ParseInt(strings.TrimSuffix(v, "m"), 10, 64)
		return n
	}
	f, _ := strconv.ParseFloat(v, 64)
	return int64(f * 1000)
}

func parseMemory(v string) int64 {
	if v == "" {
		return 0
	}
	switch {
	case strings.HasSuffix(v, "Gi"):
		n, _ := strconv.ParseFloat(strings.TrimSuffix(v, "Gi"), 64)
		return int64(n * 1024)
	case strings.HasSuffix(v, "Mi"):
		n, _ := strconv.ParseFloat(strings.TrimSuffix(v, "Mi"), 64)
		return int64(n)
	}
	return 0
}
