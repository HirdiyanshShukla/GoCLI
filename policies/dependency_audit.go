package policies

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"devsandbox/core/policy"
)

// DependencyAudit checks for banned or deprecated packages in the dependency file.
type DependencyAudit struct{}

func (p *DependencyAudit) Name() string        { return "dependency-audit" }
func (p *DependencyAudit) DisplayName() string { return "Dependency Audit" }
func (p *DependencyAudit) Category() string    { return "security" }
func (p *DependencyAudit) Severity() string    { return "error" }
func (p *DependencyAudit) Description() string {
	return "Audits dependency files for known banned or deprecated packages. Python: pycrypto, md5. Node: request, node-uuid, crypto."
}

// bannedPython maps banned Python package names to a human-readable reason.
var bannedPython = map[string]string{
	"pycrypto": "replaced by pycryptodome",
	"md5":      "insecure hashing algorithm — use hashlib",
}

// bannedNode maps banned Node package names to a human-readable reason.
var bannedNode = map[string]string{
	"request":   "deprecated — use node-fetch or axios",
	"node-uuid": "replaced by uuid",
	"crypto":    "use Node.js built-in crypto module",
}

func (p *DependencyAudit) Run(projectPath string, _ map[string]map[string]interface{}) policy.PolicyResult {
	result := policy.PolicyResult{
		PolicyName: p.Name(),
		Severity:   p.Severity(),
		Passed:     true,
	}

	framework := detectFramework(projectPath)

	switch framework {
	case "django", "fastapi", "python":
		return p.auditPython(projectPath, result)
	case "expressjs", "react", "node":
		return p.auditNode(projectPath, result)
	case "java_springboot":
		// No banned packages for Java currently
		result.Message = "audit passed"
		return result
	default:
		result.Passed = false
		result.Findings = []policy.Finding{{Detail: "could not find dependency file — skipping audit."}}
		return result
	}
}

func (p *DependencyAudit) auditPython(projectPath string, result policy.PolicyResult) policy.PolicyResult {
	depFile := filepath.Join(projectPath, "requirements.txt")
	lines := readLines(depFile)
	if lines == nil {
		result.Passed = false
		result.Findings = []policy.Finding{{Detail: "could not find dependency file — skipping audit."}}
		return result
	}

	for lineNum, line := range lines {
		pkg := strings.ToLower(strings.TrimSpace(strings.Split(line, "==")[0]))
		pkg = strings.TrimSpace(strings.Split(pkg, ">=")[0])
		pkg = strings.TrimSpace(strings.Split(pkg, "<=")[0])
		pkg = strings.TrimSpace(strings.Split(pkg, "[")[0])
		if reason, banned := bannedPython[pkg]; banned {
			result.Passed = false
			result.Findings = append(result.Findings, policy.Finding{
				File:   "requirements.txt",
				Line:   lineNum + 1,
				Detail: pkg + " is banned — " + reason,
			})
		}
	}
	return result
}

func (p *DependencyAudit) auditNode(projectPath string, result policy.PolicyResult) policy.PolicyResult {
	depFile := filepath.Join(projectPath, "package.json")
	data, err := os.ReadFile(depFile)
	if err != nil {
		result.Passed = false
		result.Findings = []policy.Finding{{Detail: "could not find dependency file — skipping audit."}}
		return result
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		result.Passed = false
		result.Findings = []policy.Finding{{Detail: "could not parse package.json — skipping audit."}}
		return result
	}

	allDeps := make(map[string]string)
	for k, v := range pkg.Dependencies {
		allDeps[k] = v
	}
	for k, v := range pkg.DevDependencies {
		allDeps[k] = v
	}

	for name := range allDeps {
		if reason, banned := bannedNode[name]; banned {
			result.Passed = false
			result.Findings = append(result.Findings, policy.Finding{
				File:   "package.json",
				Detail: name + " is banned — " + reason,
			})
		}
	}
	return result
}
