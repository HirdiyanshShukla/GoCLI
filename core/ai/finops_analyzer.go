package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FinOpsMutation is one structural suggestion from the AI. OldValue is the
// literal string "not set" when the AI is proposing to ADD a field that
// does not currently exist, rather than modify an existing one.
type FinOpsMutation struct {
	ContainerName string `json:"container_name"`
	FieldPath     string `json:"field_path"` // "requests.cpu" | "requests.memory" | "limits.cpu" | "limits.memory"
	OldValue      string `json:"old_value"`
	NewValue      string `json:"new_value"`
	Reasoning     string `json:"reasoning"`
	ChangeType    string `json:"change_type"` // "waste" | "risk"
}

// AnalyzeFinOps asks Gemini for qualitative resource-allocation suggestions.
// The AI NEVER sees or computes dollar amounts; only resource values and
// container names, validated against the manifest it was actually shown.
func AnalyzeFinOps(manifestYAML string, framework string, containerNames []string) ([]FinOpsMutation, error) {
	client, err := NewClient()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	systemPrompt := fmt.Sprintf(`You are a Principal Cloud Architect and Kubernetes FinOps Expert.
Your objective is to analyze Kubernetes resource allocations (CPU/Memory requests and limits) for a given deployment and suggest safe, qualitative optimizations.

Rules:
1. DO NOT compute, estimate, or output dollar amounts or pricing calculations.
2. Analyze the relationship between the framework context (%s), the primary container, and any sidecars.
3. Suggest changes for two distinct reasons:
   a) "waste" - the allocation is higher than the workload needs (lower it to save cost)
   b) "risk" - the allocation is dangerously low and risks instability/OOM (raise it for safety, even if this costs more)
4. Set "change_type" to either "waste" or "risk" so the cost delta can be labeled correctly.
5. Only reference container names from this exact list - never invent one: %s
6. If a field is not currently set in the manifest, set old_value to the literal string "not set" and propose adding it, rather than guessing at a current value.
7. Output your suggestions strictly as structural mutations - never as shell commands, never as YAML snippets to paste.

Respond ONLY with a valid JSON array of mutation objects matching this schema:
[
  {
    "container_name": "string",
    "field_path": "requests.cpu | requests.memory | limits.cpu | limits.memory",
    "old_value": "string",
    "new_value": "string",
    "reasoning": "string - concise, technical explanation of WHY this is safe and necessary",
    "change_type": "waste | risk"
  }
]
If no changes are warranted, respond with an empty array: []`,
		framework, strings.Join(containerNames, ", "))

	userMessage := fmt.Sprintf("Analyze this rendered Kubernetes manifest:\n\n%s", manifestYAML)

	responseText, err := client.Complete(systemPrompt, userMessage)
	if err != nil {
		return nil, fmt.Errorf("FinOps analysis failed: %w", err)
	}

	cleaned := strings.TrimSpace(responseText)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var mutations []FinOpsMutation
	if err := json.Unmarshal([]byte(cleaned), &mutations); err != nil {
		return nil, fmt.Errorf("AI returned invalid JSON: %w", err)
	}

	// Discard any mutation referencing a container not in the real list;
	// defense in depth even though the prompt already constrains this.
	var filtered []FinOpsMutation
	for _, m := range mutations {
		for _, name := range containerNames {
			if m.ContainerName == name {
				filtered = append(filtered, m)
				break
			}
		}
	}
	return filtered, nil
}
