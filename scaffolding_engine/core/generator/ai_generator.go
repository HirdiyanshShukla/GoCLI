package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"devsandbox/core/ai"
	"devsandbox/scaffolding_engine/core/detector"
	"devsandbox/scaffolding_engine/core/rules"
)

// AIGeneratorResult holds the output of AI Dockerfile generation.
type AIGeneratorResult struct {
	DockerfileContent string
}

// ── dependency file map ───────────────────────────────────────────────────────
// Maps a framework name to the ordered list of dependency/config files that
// give the AI the most accurate information for generating a Dockerfile.
var dependencyFilesForFramework = map[string][]string{
	// Python: Add Poetry and UV lockfiles
	"python":  {"requirements.txt", "pyproject.toml", "poetry.lock", "uv.lock"},
	"django":  {"requirements.txt", "pyproject.toml", "poetry.lock", "uv.lock"},
	"fastapi": {"requirements.txt", "pyproject.toml", "poetry.lock", "uv.lock"},

	// Go: Add workspaces
	"go": {"go.mod", "go.work"},

	// Java, Rust, Ruby
	"java_springboot": {"pom.xml", "build.gradle"},
	"rust":            {"Cargo.toml"},
	"ruby":            {"Gemfile"},

	// Node.js Ecosystem: Add all lockfiles
	"expressjs": {"package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb"},
	"nestjs":    {"package.json", "nest-cli.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb"},
	"nextjs":    {"package.json", "next.config.js", "next.config.ts", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb"},
	"nodejs":    {"package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb"},
	"react":     {"package.json", "vite.config.js", "vite.config.ts", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb"},
	"vue":       {"package.json", "vite.config.js", "vite.config.ts", "vue.config.js", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb"},
	"angular":   {"package.json", "angular.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb"},
	"svelte":    {"package.json", "svelte.config.js", "vite.config.js", "vite.config.ts", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb"},
}

// ── context builder ───────────────────────────────────────────────────────────

// buildDockerfileContext assembles all project-specific facts into clearly-
// labelled sections so the AI can generate a precise, project-aware Dockerfile
// rather than guessing from a generic framework name alone.
func buildDockerfileContext(
	projectPath string,
	vars map[string]interface{},
	aiResult *detector.AIDetectionResult,
) string {
	var sb strings.Builder
	
	// Normalizing framework key to ensure proper lockfile detection
	rawFw := strings.ToLower(fmt.Sprintf("%v", vars["framework"]))
	aliases := map[string]string{
		"golang": "go", "node": "nodejs", "node.js": "nodejs",
		"vuejs": "vue", "spring": "java_springboot", "springboot": "java_springboot",
	}
	framework := rawFw
	if alias, ok := aliases[rawFw]; ok {
		framework = alias
	}

	// ── 1. Dependency files ───────────────────────────────────────────────────
	sb.WriteString("=== DEPENDENCY FILES ===\n")
	candidates := dependencyFilesForFramework[framework]
	if len(candidates) == 0 {
		// Unknown framework — try all well-known manifests
		candidates = []string{
			"go.mod", "requirements.txt", "pyproject.toml",
			"package.json", "pom.xml", "build.gradle",
			"Cargo.toml", "Gemfile",
		}
	}
	depFilesRead := 0
	for _, name := range candidates {
		data, err := os.ReadFile(filepath.Join(projectPath, name))
		if err != nil {
			continue
		}
		content := string(data)
		if len(content) > 5000 {
			content = content[:5000] + "\n...[truncated]"
		}
		sb.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", name, content))
		depFilesRead++
	}
	if depFilesRead == 0 {
		sb.WriteString("(no standard dependency files found)\n")
	}

	// ── 2. Python version pin files ───────────────────────────────────────────
	if strings.Contains(framework, "python") ||
		framework == "django" || framework == "fastapi" {
		for _, vf := range []string{".python-version", "runtime.txt"} {
			data, err := os.ReadFile(filepath.Join(projectPath, vf))
			if err == nil {
				sb.WriteString(fmt.Sprintf(
					"\n--- %s (Python version pin) ---\n%s\n",
					vf, strings.TrimSpace(string(data)),
				))
				break // only need one
			}
		}
	}

	// ── 3. Entry point file content ───────────────────────────────────────────
	if aiResult != nil && aiResult.EntryPath != "" {
		entryPath := filepath.Join(projectPath, aiResult.EntryPath)
		data, err := os.ReadFile(entryPath)
		if err == nil {
			content := string(data)
			if len(content) > 3000 {
				content = content[:3000] + "\n...[truncated]"
			}
			sb.WriteString(fmt.Sprintf(
				"\n=== ENTRY POINT: %s ===\n%s\n",
				aiResult.EntryPath, content,
			))
		}
	}

	// ── 4. Build scripts ──────────────────────────────────────────────────────
	sb.WriteString("\n=== BUILD SCRIPTS ===\n")
	buildScriptsFound := 0
	for _, script := range []string{"Makefile", "scripts/build.sh", "Taskfile.yml", "build.sh"} {
		data, err := os.ReadFile(filepath.Join(projectPath, script))
		if err != nil {
			continue
		}
		content := string(data)
		if len(content) > 1500 {
			content = content[:1500] + "\n...[truncated]"
		}
		sb.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", script, content))
		buildScriptsFound++
	}
	if buildScriptsFound == 0 {
		sb.WriteString("(no build scripts found)\n")
	}

	// ── 4.5 Dockerignore ──────────────────────────────────────────────────────
	if data, err := os.ReadFile(filepath.Join(projectPath, ".dockerignore")); err == nil {
		sb.WriteString("\n=== .DOCKERIGNORE ===\n")
		sb.WriteString(string(data) + "\n")
	}

	// ── 4.7 Build-Time Environment Variables ──────────────────────────────────
	for _, ef := range []string{".env.example", ".env.sample"} {
		if data, err := os.ReadFile(filepath.Join(projectPath, ef)); err == nil {
			sb.WriteString(fmt.Sprintf("\n=== %s (VARIABLE NAMES ONLY — use as ARG) ===\n%s\n", ef, string(data)))
		}
	}

	// ── 5. Shallow project tree (top 3 levels) ────────────────────────────────
	sb.WriteString("\n=== PROJECT STRUCTURE (top 3 levels) ===\n")
	buildShallowTree(&sb, projectPath, projectPath, 0, 3)

	return sb.String()
}

// buildShallowTree writes a simple indented file tree up to maxDepth levels,
// skipping large generated directories that add noise without signal.
func buildShallowTree(sb *strings.Builder, root, current string, depth, maxDepth int) {
	if depth > maxDepth {
		return
	}
	ignoreDirs := map[string]bool{
		"node_modules": true, ".git": true, "venv": true, ".venv": true,
		"env": true, "__pycache__": true, ".cache": true, "vendor": true,
		"dist": true, "build": true, "target": true, ".next": true,
		"coverage": true, ".angular": true, // .angular is Angular's large build cache
	}
	entries, err := os.ReadDir(current)
	if err != nil {
		return
	}
	for _, e := range entries {
		if ignoreDirs[e.Name()] {
			continue
		}
		indent := strings.Repeat("  ", depth)
		if e.IsDir() {
			sb.WriteString(fmt.Sprintf("%s📁 %s/\n", indent, e.Name()))
			buildShallowTree(sb, root, filepath.Join(current, e.Name()), depth+1, maxDepth)
		} else {
			sb.WriteString(fmt.Sprintf("%s   %s\n", indent, e.Name()))
		}
	}
}

// ── markdown fence stripper ───────────────────────────────────────────────────

func stripDockerfileMarkdownFences(content string) string {
	content = strings.TrimSpace(content)

	if strings.HasPrefix(content, "```") {
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = content[idx+1:]
		}
	}

	content = strings.TrimRight(content, "\n \t")
	if strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimRight(content, "\n \t")
	}

	return strings.TrimSpace(content)
}

// ── structural validator ──────────────────────────────────────────────────────

type dockerfileValidation struct {
	HasFrom        bool
	ExposedPorts   []int
	HasUser        bool
	UserIsRoot     bool
	HasWorkdir     bool
	HasCmd         bool
	HasEntrypoint  bool
	HasHealthcheck bool
	StageCount     int
}

func parseDockerfileDirectives(content string) dockerfileValidation {
	var v dockerfileValidation
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "FROM "):
			v.HasFrom = true
			v.StageCount++

		case strings.HasPrefix(upper, "EXPOSE "):
			parts := strings.Fields(line[7:])
			for _, part := range parts {
				portStr := strings.Split(part, "/")[0]
				if p, err := strconv.Atoi(portStr); err == nil {
					v.ExposedPorts = append(v.ExposedPorts, p)
				}
			}

		case strings.HasPrefix(upper, "USER "):
			v.HasUser = true
			userVal := strings.ToLower(strings.TrimSpace(line[5:]))
			if userVal == "root" || userVal == "0" ||
				strings.HasPrefix(userVal, "root:") || strings.HasSuffix(userVal, ":0") {
				v.UserIsRoot = true
			} else {
				v.UserIsRoot = false
			}

		case strings.HasPrefix(upper, "WORKDIR "):
			v.HasWorkdir = true

		case strings.HasPrefix(upper, "CMD "):
			v.HasCmd = true

		case strings.HasPrefix(upper, "ENTRYPOINT "):
			v.HasEntrypoint = true

		case strings.HasPrefix(upper, "HEALTHCHECK ") &&
			!strings.Contains(upper, "HEALTHCHECK NONE"):
			v.HasHealthcheck = true
		}
	}
	return v
}

func validateDockerfileStructure(content string, expectedPort int) (errors, warnings []string) {
	v := parseDockerfileDirectives(content)

	if !v.HasFrom {
		errors = append(errors, "missing FROM directive")
	}
	
	isUnprivilegedBase := strings.Contains(strings.ToLower(content), "nginx-unprivileged")
	
	if !v.HasUser && !isUnprivilegedBase {
		errors = append(errors, "missing USER directive — the final stage MUST set a non-root user (create appgroup/appuser and add 'USER appuser')")
	} else if v.UserIsRoot {
		errors = append(errors, "USER root or USER 0 is forbidden — the container MUST run as a non-root user")
	}
	if !v.HasCmd && !v.HasEntrypoint && !isUnprivilegedBase {
		errors = append(errors, "missing CMD or ENTRYPOINT — the container has no start command")
	}

	portMatched := false
	for _, p := range v.ExposedPorts {
		if p == expectedPort {
			portMatched = true
			break
		}
	}
	if len(v.ExposedPorts) == 0 {
		errors = append(errors, fmt.Sprintf("missing EXPOSE directive — MUST add 'EXPOSE %d'", expectedPort))
	} else if !portMatched {
		errors = append(errors, fmt.Sprintf(
			"EXPOSE %v does not match the required port %d — change it to 'EXPOSE %d'",
			v.ExposedPorts, expectedPort, expectedPort,
		))
	}

	if !v.HasWorkdir {
		warnings = append(warnings, "no WORKDIR set — working directory defaults to /")
	}
	if !v.HasHealthcheck {
		warnings = append(warnings, "no HEALTHCHECK directive — consider adding one for Kubernetes readiness")
	}

	return errors, warnings
}

// validateCopySourcesExist parses COPY/ADD instructions (excluding cross-stage copies) 
// and verifies every referenced source actually exists in the project.
func validateCopySourcesExist(content, projectPath string) []string {
	var errors []string
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		upper := strings.ToUpper(line)
		if !strings.HasPrefix(upper, "COPY ") && !strings.HasPrefix(upper, "ADD ") {
			continue
		}
		if strings.Contains(line, "--from=") {
			continue // cross-stage copy — nothing to check on disk
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		var pathTokens []string
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "--") {
				continue
			}
			pathTokens = append(pathTokens, f)
		}
		if len(pathTokens) < 2 {
			continue
		}
		sources := pathTokens[:len(pathTokens)-1] // last token is the destination
		for _, src := range sources {
			if src == "." || src == "./" {
				continue
			}
			if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
				continue
			}
			fullPattern := filepath.Join(projectPath, src)
			matches, _ := filepath.Glob(fullPattern)
			if len(matches) == 0 {
				if _, statErr := os.Stat(fullPattern); statErr != nil {
					errors = append(errors, fmt.Sprintf(
						"COPY/ADD references '%s' which does not exist in the project — remove it or fix the path", src))
				}
			}
		}
	}
	return errors
}

// ── hadolint wrapper ──────────────────────────────────────────────────────────

type hadolintViolation struct {
	Line    int    `json:"line"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Level   string `json:"level"`
}

func lintGeneratedDockerfile(content string) (violations []string, skipped bool) {
	if err := exec.Command("docker", "info").Run(); err != nil {
		return nil, true // Docker not running — skip hadolint gracefully
	}

	tmp, err := os.CreateTemp("", "Dockerfile-lint-*.tmp")
	if err != nil {
		return nil, true
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return nil, true
	}
	tmp.Close()

	out, _ := exec.Command(
		"docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/tmp/Dockerfile:ro", tmp.Name()),
		"hadolint/hadolint",
		"hadolint",
		"--format", "json",
		"--ignore", "DL3008", 
		"--ignore", "DL3009", 
		"--ignore", "DL3015", 
		"/tmp/Dockerfile",
	).Output()

	if len(out) == 0 {
		return nil, false
	}

	var raw []hadolintViolation
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, false 
	}

	for _, v := range raw {
		violations = append(violations, fmt.Sprintf(
			"[%s] Line %d (%s): %s",
			v.Code, v.Line, v.Level, v.Message,
		))
	}
	return violations, false
}

// ── public entry point ────────────────────────────────────────────────────────

func AIGenerateDockerfileWithValidation(
	projectPath string,
	vars map[string]interface{},
	aiResult *detector.AIDetectionResult,
) (AIGeneratorResult, error) {
	const maxAttempts = 3

	var lastResult AIGeneratorResult
	var priorErrors []string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt == 1 {
			fmt.Println("\033[1;36m🤖 Generating Dockerfile from project context...\033[0m")
		} else {
			fmt.Printf(
				"\033[1;33m🔄 Attempt %d/%d — sending %d structural fix(es) back to AI...\033[0m\n",
				attempt, maxAttempts, len(priorErrors),
			)
		}

		result, err := generateDockerfileOnce(projectPath, vars, aiResult, priorErrors)
		if err != nil {
			return result, err
		}
		lastResult = result

		// ── Level A: structural validation ────────────────────────────────────
		expectedPort := 8080
		
		if pFloat, ok := vars["app_port"].(float64); ok {
			expectedPort = int(pFloat)
		} else if pInt, ok := vars["app_port"].(int); ok {
			expectedPort = pInt
		} else if pStr, ok := vars["app_port"].(string); ok {
			if parsed, err := strconv.Atoi(pStr); err == nil {
				expectedPort = parsed
			}
		}
		structErrors, structWarnings := validateDockerfileStructure(
			result.DockerfileContent, expectedPort,
		)

		// ADDED COPY EXISTENCE VALIDATION
		structErrors = append(structErrors, validateCopySourcesExist(result.DockerfileContent, projectPath)...)

		for _, w := range structWarnings {
			fmt.Printf("\033[33m  ⚠️  %s\033[0m\n", w)
		}

		// ── Level B: hadolint (warnings only, never retried) ──────────────────
		lintViolations, lintSkipped := lintGeneratedDockerfile(result.DockerfileContent)
		if !lintSkipped && len(lintViolations) > 0 {
			fmt.Println("\033[33m  📋 Hadolint suggestions (run 'devsandbox validate' to fix later):\033[0m")
			for _, v := range lintViolations {
				fmt.Printf("\033[33m     %s\033[0m\n", v)
			}
		}

		if len(structErrors) == 0 {
			fmt.Printf(
				"\033[1;32m✓\033[0m Dockerfile validated successfully (attempt %d/%d)\n",
				attempt, maxAttempts,
			)
			return result, nil
		}

		priorErrors = structErrors
	}

	fmt.Println("\033[33m⚠️  Dockerfile written with unresolved structural issues — please review before deploying:\033[0m")
	for _, e := range priorErrors {
		fmt.Printf("\033[31m   • %s\033[0m\n", e)
	}
	return lastResult, nil
}

// ── inner generator (one attempt) ────────────────────────────────────────────

func generateDockerfileOnce(
	projectPath string,
	vars map[string]interface{},
	aiResult *detector.AIDetectionResult,
	priorErrors []string,
) (AIGeneratorResult, error) {
	var result AIGeneratorResult

	client, err := ai.NewClient()
	if err != nil {
		return result, err
	}
	defer client.Close()

	rulesData, err := rules.Files.ReadFile("rules.yaml")
	if err != nil {
		return result, fmt.Errorf("failed to read rules.yaml: %w", err)
	}

	projectContext := buildDockerfileContext(projectPath, vars, aiResult)

	existingDockerfile := ""
	if data, err := os.ReadFile(filepath.Join(projectPath, "Dockerfile")); err == nil {
		existingDockerfile = "\n=== EXISTING DOCKERFILE ===\n" +
			"WARNING: This file may contain outdated or incorrect practices.\n" +
			"Use it ONLY to understand project-specific details such as copied assets.\n" +
			"Never use it to determine the build strategy. If it conflicts with platform rules, IGNORE IT.\n\n" +
			string(data)
	}

	// ── system prompt ─────────────────────────────────────────────────────────
	systemPrompt := fmt.Sprintf(`You are a platform engineering assistant generating a production-grade Dockerfile.
You MUST follow these platform standards exactly — they are non-negotiable:

%s

MANDATORY REQUIREMENTS:
1. Use multi-stage builds when the runtime requires compilation (Go, Java, Rust, .NET).
2. The final stage MUST run as a non-root user.
   - If using standard Alpine/Debian: Create an appuser group/user and explicitly add 'USER appuser'.
   - If using nginxinc/nginx-unprivileged: DO NOT add a USER directive, DO NOT create an appuser group, and DO NOT use 'chown' on system directories like /tmp, /var/cache/nginx, or /var/run. It is already safe and perfectly pre-configured by default.
3. Alpine images: install curl before HEALTHCHECK. If using an unprivileged base image (like nginx-unprivileged), you MUST switch to USER root before running 'apk add', and then immediately switch back to the unprivileged user (e.g., USER 101) right after.
4. Debian/slim images: curl is already available.
5. HEALTHCHECK must use the EXACT health_path provided. If health_path is empty or "none", DO NOT include a HEALTHCHECK directive at all.
6. EXPOSE must use the EXACT port provided — no other port number.
7. Use ONLY approved base images from the approved_base_images list in the rules.
8. readOnlyRootFilesystem is enforced by Kubernetes. Your Dockerfile MUST NOT write
   outside /tmp at runtime (no log files to /app/logs, no pid files, etc.).
9. The dockerfile_content JSON value must be a valid escaped string: escape all
   double quotes as \" and all backslashes as \\.
10. If build-time env vars are required (e.g., VITE_*, NEXT_PUBLIC_* found in .env.example), declare them as ARG in the build stage. Never hardcode secrets.

System Directory Rule: NEVER attempt to run chown on standard Linux system directories (like /tmp, /var, or /usr). If a framework (like Next.js) requires a writable cache folder inside /tmp, you must create a specific subdirectory (e.g., RUN mkdir -p /tmp/.next_cache) and run chown ONLY on that specific subdirectory.

CONTEXT USAGE RULES — YOU MUST FOLLOW THESE:
- Directory Ownership Rule: If you switch to a non-root user (e.g., 'appuser') in the build stage, you MUST explicitly grant that user ownership of the working directory (e.g., RUN chown -R appuser:appgroup /app) BEFORE executing any package manager install commands, otherwise they will fail with EACCES permission denied.
- Version Rule: Extract the EXACT runtime version from the provided files.
  go.mod "go 1.22.4" → FROM golang:1.22-alpine (not latest, not 1.21).
  .python-version "3.11.9" → FROM python:3.11-slim.
- Dependency Rule: Inspect dependency files for C-binding libraries.
  If found, install the required OS packages (gcc, musl-dev, libpq-dev) in the BUILD stage.
- Production Run Rule: The provided "Local command" is for development. You MUST translate this into a PRODUCTION-READY startup command.
  * UNIVERSAL PORT RULE: If the framework's CLI requires explicitly defining the host and port, you MUST bind to '0.0.0.0' and the exact hardcoded Port provided in REQUIRED PARAMETERS. NEVER use environment variables like $PORT.
  * Compiled (Go, Rust): The final CMD MUST execute the compiled binary directly (e.g., CMD ["./server"]). NEVER use 'go run' or 'cargo run' in the final stage.
  * JVM (Java, Kotlin): The final CMD MUST execute the compiled jar (e.g., CMD ["java", "-jar", "app.jar"]). NEVER use build tools like 'mvn spring-boot:run' or 'gradlew bootRun'.
  * Node.js: The final CMD MUST execute the main file directly using the node runtime (e.g., CMD ["node", "dist/main.js"] or CMD ["npm", "start"] if start runs node). NEVER use 'nodemon' or 'npm run dev'.
  * Python: Translate dev servers (manage.py runserver) to production servers (gunicorn/uvicorn). Bind exactly to 0.0.0.0:<EXPOSE_PORT> (e.g., --bind 0.0.0.0:8000).
  * Ruby: Translate 'rails s' or 'rackup' to production servers (puma/unicorn). Bind exactly to 0.0.0.0:<EXPOSE_PORT> (e.g., -b tcp://0.0.0.0:8080).
  * SPA (React, Vue, Angular): Serve built assets using nginxinc/nginx-unprivileged:alpine. If you must generate an Nginx config file, write ONLY a server { ... } block to /etc/nginx/conf.d/default.conf. NEVER output events or http blocks.
- Strict File Existence Rule: NEVER reference a file in a COPY or CMD instruction unless it explicitly exists in the provided project tree. Do not invent filenames.
- Entrypoint Rule: Use the provided Entry Point file strictly as the application's entrypoint. Do not search for another main package.
- Healthcheck Rule: Your HEALTHCHECK MUST strictly call: curl -f http://localhost:<EXPOSE_PORT><health_path>
- Package Manager Rule: Use the exact install command corresponding to the discovered lockfile.
  CRITICAL: Any global tool installation (like 'npm install -g pnpm', yarn, or bun) MUST be executed in the build stage while the user is still 'root', BEFORE you switch to the non-root appuser. 
  package-lock.json → npm ci
  pnpm-lock.yaml    → pnpm install --frozen-lockfile (Requires global pnpm installed as root first)
  yarn.lock         → yarn install --frozen-lockfile (Requires global yarn installed as root first)
  bun.lockb         → bun install --frozen-lockfile (Requires global bun installed as root first)

Respond with ONLY valid JSON: {"dockerfile_content": "<full Dockerfile as a JSON string>"}`,
		string(rulesData),
	)

	// ── user message ─────────────────────────────────────────────────────────
	framework := fmt.Sprintf("%v", vars["framework"])
	appPort := vars["app_port"]
	runCommand := fmt.Sprintf("%v", vars["run_command"])
	testCommand := fmt.Sprintf("%v", vars["test_command"])

	healthPath := fmt.Sprintf("%v", vars["health_path"])
	healthDisplay := healthPath
	if healthPath == "" || healthPath == "<nil>" || healthPath == "none" {
		healthDisplay = "(none — omit HEALTHCHECK entirely)"
	}

	correctionBlock := ""
	if len(priorErrors) > 0 {
		var cb strings.Builder
		cb.WriteString("\n=== PREVIOUS ATTEMPT FAILED VALIDATION — FIX ALL OF THE FOLLOWING ===\n")
		for i, e := range priorErrors {
			cb.WriteString(fmt.Sprintf("%d. [STRUCTURAL ERROR] %s\n", i+1, e))
		}
		cb.WriteString("Generate a corrected Dockerfile that resolves every issue above.\n")
		correctionBlock = cb.String()
	}

	userMessage := fmt.Sprintf(
		`Generate a production Dockerfile for this project.

REQUIRED PARAMETERS (hardcoded — do not change these values):
- Framework    : %s
- Port         : %v  ← EXPOSE must be exactly this
- Health path  : %s  ← HEALTHCHECK must use exactly this path
- Local command: %s  ← Translate this to a PRODUCTION CMD
- Test command : %s

PROJECT CONTEXT (use every section to make your Dockerfile accurate — do not ignore):
%s
%s
%s`,
		framework, appPort, healthDisplay, runCommand, testCommand,
		projectContext, existingDockerfile, correctionBlock,
	)

	responseJSON, err := client.CompleteWithRetry(systemPrompt, userMessage, 3)
	if err != nil {
		return result, fmt.Errorf("AI Dockerfile generation failed: %w", err)
	}

	// ── parse response ────────────────────────────────────────────────────────
	cleaned := strings.TrimSpace(responseJSON)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var parsed struct {
		Content string `json:"dockerfile_content"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		preview := responseJSON
		if len(preview) > 300 {
			preview = preview[:300] + "...[truncated]"
		}
		return result, fmt.Errorf("failed to parse AI Dockerfile JSON response: %w\nRaw: %s", err, preview)
	}

	dockerfileContent := stripDockerfileMarkdownFences(parsed.Content)

	result.DockerfileContent = dockerfileContent
	return result, nil
}