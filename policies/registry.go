package policies

import "devsandbox/core/policy"

// All returns the full registry of available policies keyed by their Name().
func All() map[string]policy.Policy {
	return map[string]policy.Policy{
		// Existing
		"no-hardcoded-secrets":     &NoHardcodedSecrets{},
		"health-endpoint":          &HealthEndpoint{},
		"dependency-audit":         &DependencyAudit{},
		"feature-flags":            &FeatureFlags{},
		"api-versioning":           &ApiVersioning{},
		"logging-standard":         &LoggingStandard{},
		// Phase 1 additions
		"no-latest-tag":            &NoLatestTag{},
		"non-root-container":       &NonRootContainer{},
		"mandatory-probes":         &MandatoryProbes{},
		"resource-limits":          &ResourceLimits{},
		"approved-registries":      &ApprovedRegistries{},
		"no-privileged-containers": &NoPrivilegedContainers{},
		"env-var-size-limit":       &EnvVarSize{},
	}
}
