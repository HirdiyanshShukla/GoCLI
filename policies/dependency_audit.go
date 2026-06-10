package policies

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devsandbox/core/policy"
)

type DependencyAudit struct{}

func (p *DependencyAudit) Name() string        { return "dependency-audit" }
func (p *DependencyAudit) DisplayName() string { return "Dependency Audit" }
func (p *DependencyAudit) Category() string    { return "security" }
func (p *DependencyAudit) Severity() string    { return "error" }
func (p *DependencyAudit) Description() string {
	return "Audits dependency files for banned or deprecated packages and verifies if they are actively imported. Supports Python, Node.js, and Java."
}

var bannedPython = map[string]string{
	"pycrypto": "replaced by pycryptodome",
	"md5":      "insecure hashing algorithm — use hashlib",
}

var bannedNode = map[string]string{
	"request":   "deprecated — use node-fetch or axios",
	"node-uuid": "replaced by uuid",
	"crypto":    "use Node.js built-in crypto module",
}

var bannedJava = map[string]string{
	"log4j": "vulnerable to Log4Shell — use log4j-api >= 2.17.1 or slf4j",
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
		return p.auditJava(projectPath, result)
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
			usedInCode := false
			walkSourceFiles(projectPath, func(path string) {
				if usedInCode || !strings.HasSuffix(path, ".py") {
					return
				}
				srcLines := readLines(path)
				for _, srcLine := range srcLines {
					lowerLine := strings.ToLower(srcLine)
					if strings.Contains(lowerLine, "import "+pkg) || strings.Contains(lowerLine, "from "+pkg) {
						usedInCode = true
						return
					}
				}
			})

			severity := "declared in dependencies"
			if usedInCode {
				severity = "declared AND actively imported in source code"
			}

			result.Passed = false
			result.Findings = append(result.Findings, policy.Finding{
				File:   "requirements.txt",
				Line:   lineNum + 1,
				Detail: pkg + " is banned (" + reason + ") — " + severity,
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
			usedInCode := false
			walkSourceFiles(projectPath, func(path string) {
				if usedInCode || (!strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".ts")) {
					return
				}
				srcLines := readLines(path)
				for _, srcLine := range srcLines {
					lowerLine := strings.ToLower(srcLine)
					if strings.Contains(lowerLine, fmt.Sprintf("require('%s')", name)) ||
						strings.Contains(lowerLine, fmt.Sprintf("require(\"%s\")", name)) ||
						strings.Contains(lowerLine, fmt.Sprintf("from '%s'", name)) ||
						strings.Contains(lowerLine, fmt.Sprintf("from \"%s\"", name)) {
						usedInCode = true
						return
					}
				}
			})

			severity := "declared in dependencies"
			if usedInCode {
				severity = "declared AND actively imported in source code"
			}

			result.Passed = false
			result.Findings = append(result.Findings, policy.Finding{
				File:   "package.json",
				Detail: name + " is banned (" + reason + ") — " + severity,
			})
		}
	}
	return result
}

func (p *DependencyAudit) auditJava(projectPath string, result policy.PolicyResult) policy.PolicyResult {
	depFile := filepath.Join(projectPath, "pom.xml")
	data, err := os.ReadFile(depFile)
	if err != nil {
		depFile = filepath.Join(projectPath, "build.gradle")
		data, err = os.ReadFile(depFile)
		if err != nil {
			result.Passed = false
			result.Findings = []policy.Finding{{Detail: "could not find pom.xml or build.gradle — skipping audit."}}
			return result
		}
	}

	content := strings.ToLower(string(data))

	for name, reason := range bannedJava {
		if strings.Contains(content, name) {
			usedInCode := false
			walkSourceFiles(projectPath, func(path string) {
				if usedInCode || !strings.HasSuffix(path, ".java") {
					return
				}
				srcLines := readLines(path)
				for _, srcLine := range srcLines {
					if strings.Contains(srcLine, "import ") && strings.Contains(strings.ToLower(srcLine), name) {
						usedInCode = true
						return
					}
				}
			})

			severity := "declared in build file"
			if usedInCode {
				severity = "declared AND actively imported in source code"
			}

			result.Passed = false
			result.Findings = append(result.Findings, policy.Finding{
				File:   filepath.Base(depFile),
				Detail: name + " is banned (" + reason + ") — " + severity,
			})
		}
	}
	return result
}
