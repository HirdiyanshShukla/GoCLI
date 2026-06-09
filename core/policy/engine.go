package policy

import (
	"devsandbox/core/config"
)

// RunPolicies executes all applicable policies according to the mode in cfg.
//
// opt-out (default): start with all registered policies, remove disabled ones.
// opt-in:            start with empty set, only run explicitly named policies
//                    from cfg.Policies.Enabled.
//
// The engine never calls os.Exit — results are returned to the command layer.
func RunPolicies(projectPath string, cfg config.PipelineConfig, registry map[string]Policy) []PolicyResult {
	var toRun []string

	mode := cfg.Policies.Mode
	if mode == "" {
		mode = "opt-out"
	}

	switch mode {
	case "opt-in":
		// Only run policies explicitly listed in the enabled field.
		enabled := make(map[string]bool)
		for _, name := range cfg.Policies.Enabled {
			enabled[name] = true
		}
		for name := range registry {
			if enabled[name] {
				toRun = append(toRun, name)
			}
		}

	default: // opt-out
		// Run everything except what is explicitly disabled.
		disabled := make(map[string]bool)
		for _, name := range cfg.Policies.Disabled {
			disabled[name] = true
		}
		for name := range registry {
			if !disabled[name] {
				toRun = append(toRun, name)
			}
		}
	}

	results := make([]PolicyResult, 0, len(toRun))
	for _, name := range toRun {
		p := registry[name]
		result := p.Run(projectPath, cfg.Policies.Config)
		result.PolicyName = p.Name()
		result.Category   = p.Category()
		result.Severity   = p.Severity()
		results = append(results, result)
	}

	return results
}