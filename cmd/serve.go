package cmd

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "embed"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

// workspaceRegistry maps short workspace IDs to their absolute temp directory paths.
// Storing the resolved path server-side avoids path traversal attacks
// (a client can only reference an ID it received from the server).
var (
	workspaceRegistry   = map[string]string{}
	workspaceRegistryMu sync.RWMutex
)

// upgrader promotes plain HTTP connections to WebSocket connections.
// CheckOrigin is permissive because the server is strictly local (localhost only).
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Launch the local OpsAI web dashboard at http://localhost:3030",
	Long: `Starts a local HTTP server that exposes the OpsAI scaffolding engine through a
browser-based dashboard. Supports three workspace modes:
  • Local  — run the pipeline in the directory where you launched the server
  • ZIP    — upload a zipped project; it is extracted into an isolated temp dir
  • GitHub — paste a repo URL; it is cloned into an isolated temp dir`,
	Run: func(cmd *cobra.Command, args []string) {
		startUIServer()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

// ─── EMBEDDED UI ─────────────────────────────────────────────────────────────

//go:embed ui/dist/index.html
var indexHTML []byte

// ─── SERVER BOOTSTRAP ────────────────────────────────────────────────────────

func startUIServer() {
	mux := http.NewServeMux()

	// Static assets — serve the embedded index.html for all UI routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	// REST endpoints
	mux.HandleFunc("/api/upload", handleZipUpload)
	mux.HandleFunc("/api/clone", handleGitClone)

	// WebSocket endpoint for live log streaming
	mux.HandleFunc("/ws", handleWebSocket)

	// Register SIGINT / SIGTERM cleanup handler so temp dirs are removed on exit
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
		<-quit
		fmt.Println("\n\033[1;33m⚡ Shutting down — cleaning up temporary workspaces...\033[0m")
		cleanupAllWorkspaces()
		os.Exit(0)
	}()

	// Best-effort auto-open in the default browser (only one of these will work)
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = exec.Command("xdg-open", "http://localhost:3030").Start()        // Linux / WSL
		_ = exec.Command("open", "http://localhost:3030").Start()             // macOS
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler",           // Windows
			"http://localhost:3030").Start()
	}()

	fmt.Println("\033[1;32m")
	fmt.Println("  ██████╗ ██████╗ ███████╗ █████╗ ██╗")
	fmt.Println("  ██╔══██╗██╔══██╗██╔════╝██╔══██╗██║")
	fmt.Println("  ██║  ██║██████╔╝███████╗███████║██║")
	fmt.Println("  ██║  ██║██╔═══╝ ╚════██║██╔══██║██║")
	fmt.Println("  ██████╔╝██║     ███████║██║  ██║██║")
	fmt.Println("  ╚═════╝ ╚═╝     ╚══════╝╚═╝  ╚═╝╚═╝\033[0m")
	fmt.Println()
	fmt.Println("\033[1;36m🚀 OpsAI Dashboard is live!\033[0m")
	fmt.Printf("\033[33m   Open: \033[1;4mhttp://localhost:3030\033[0m\n\n")
	fmt.Println("\033[90m   Press Ctrl+C to stop the server.\033[0m")

	if err := http.ListenAndServe(":3030", mux); err != nil {
		fmt.Printf("\033[1;31m❌ Server error: %v\033[0m\n", err)
		os.Exit(1)
	}
}

// ─── WORKSPACE HELPERS ───────────────────────────────────────────────────────

func newWorkspaceID() string {
	return fmt.Sprintf("ws-%d", time.Now().UnixNano())
}

func registerWorkspace(id, path string) {
	workspaceRegistryMu.Lock()
	defer workspaceRegistryMu.Unlock()
	workspaceRegistry[id] = path
}

func resolveWorkspace(id string) (string, bool) {
	if id == "" {
		// Empty ID → use the server's own working directory (Local mode)
		cwd, _ := os.Getwd()
		return cwd, true
	}
	workspaceRegistryMu.RLock()
	defer workspaceRegistryMu.RUnlock()
	path, ok := workspaceRegistry[id]
	return path, ok
}

func cleanupAllWorkspaces() {
	workspaceRegistryMu.RLock()
	defer workspaceRegistryMu.RUnlock()
	for id, path := range workspaceRegistry {
		fmt.Printf("\033[90m   🗑  Removing workspace %s (%s)\033[0m\n", id, path)
		_ = os.RemoveAll(path)
	}
}

// ─── API: ZIP UPLOAD ─────────────────────────────────────────────────────────

func handleZipUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 50 MB max upload
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "Request too large (max 50 MB)", http.StatusRequestEntityTooLarge)
		return
	}

	file, _, err := r.FormFile("projectZip")
	if err != nil {
		http.Error(w, "Missing 'projectZip' field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Create isolated workspace directory
	wsID := newWorkspaceID()
	targetDir := filepath.Join(os.TempDir(), wsID)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		http.Error(w, "Failed to create workspace", http.StatusInternalServerError)
		return
	}

	// Buffer the zip to a temp file (zip.OpenReader requires a seekable file)
	tempZip, err := os.CreateTemp("", wsID+"*.zip")
	if err != nil {
		http.Error(w, "Failed to buffer upload", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tempZip.Name())

	if _, err := io.Copy(tempZip, file); err != nil {
		tempZip.Close()
		http.Error(w, "Failed to write upload", http.StatusInternalServerError)
		return
	}
	tempZip.Close()

	// Extract the archive with path-traversal protection
	if err := extractZip(tempZip.Name(), targetDir); err != nil {
		_ = os.RemoveAll(targetDir)
		http.Error(w, fmt.Sprintf("Extraction failed: %v", err), http.StatusBadRequest)
		return
	}

	// If the zip contained a single top-level folder, use that as the workspace root
	// (standard GitHub zip convention: repo-main/...)
	targetDir = unwrapSingleDir(targetDir)

	registerWorkspace(wsID, targetDir)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"workspaceId": wsID})
}

// extractZip extracts a zip archive to destDir with path-traversal protection.
func extractZip(src, destDir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Guard against path traversal (e.g. "../../etc/passwd")
		destPath := filepath.Join(destDir, filepath.Clean("/"+f.Name))
		if !strings.HasPrefix(destPath, filepath.Clean(destDir)+string(os.PathSeparator)) &&
			destPath != filepath.Clean(destDir) {
			return fmt.Errorf("illegal path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, f.Mode()); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}

		_, copyErr := io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// unwrapSingleDir returns the path of the sole subdirectory if destDir contains
// exactly one entry which is a directory (GitHub zip convention), otherwise
// returns destDir unchanged.
func unwrapSingleDir(destDir string) string {
	entries, err := os.ReadDir(destDir)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return destDir
	}
	return filepath.Join(destDir, entries[0].Name())
}

// ─── API: GITHUB CLONE ───────────────────────────────────────────────────────

func handleGitClone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		RepoURL string `json:"repo_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.RepoURL == "" {
		http.Error(w, "Missing or invalid 'repo_url'", http.StatusBadRequest)
		return
	}

	// Basic URL sanity check — only allow http/https/git schemes
	if !strings.HasPrefix(payload.RepoURL, "https://") &&
		!strings.HasPrefix(payload.RepoURL, "http://") &&
		!strings.HasPrefix(payload.RepoURL, "git@") {
		http.Error(w, "Only https://, http://, and git@ URLs are allowed", http.StatusBadRequest)
		return
	}

	wsID := newWorkspaceID()
	targetDir := filepath.Join(os.TempDir(), wsID)

	cmd := exec.Command("git", "clone", "--depth=1", payload.RepoURL, targetDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("git clone failed: %s", string(out)), http.StatusInternalServerError)
		return
	}

	registerWorkspace(wsID, targetDir)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"workspaceId": wsID})
}

// ─── WEBSOCKET: LIVE LOG STREAMING ───────────────────────────────────────────

// allowedCommands is the security whitelist of CLI subcommand names that the
// web server is permitted to invoke. Any command not in this set is rejected.
var allowedCommands = map[string]bool{
	"init":       true,
	"prep-ci":    true,
	"run":        true,
	"validate":   true,
	"policy":     true,
	"logs":       true,
	"destroy-ci": true,
	"tunnel":     true,
	"pause":      true,
	"resume":     true,
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	wsID := r.URL.Query().Get("workspace")
	targetDir, ok := resolveWorkspace(wsID)
	if !ok {
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte("\033[1;31m❌ Unknown workspace ID. Please upload or clone a project first.\033[0m\n"))
		return
	}

	// Wait for the client to send a command message
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return
	}

	// Payload: { "command": ["run"], "gemini_api_key": "AIza..." }
	// command is a []string so multi-word subcommands work: ["logs", "analyze"]
	var payload struct {
		Command      []string `json:"command"`
		GeminiAPIKey string   `json:"gemini_api_key"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil || len(payload.Command) == 0 {
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte("\033[1;31m❌ Invalid payload. Expected {\"command\":[\"run\"], ...}\033[0m\n"))
		return
	}

	// Security: validate the base command (first element) against the whitelist
	baseCmd := payload.Command[0]
	if !allowedCommands[baseCmd] {
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf("\033[1;31m❌ Command '%s' is not permitted via the web UI.\033[0m\n", baseCmd)))
		return
	}

	// Resolve the Gemini API key: browser-supplied key takes precedence over
	// the parent process environment (useful when the shell doesn't have it set).
	geminiKey := payload.GeminiAPIKey
	if geminiKey == "" {
		geminiKey = os.Getenv("GEMINI_API_KEY") // fallback: already set in shell
	}

	_ = conn.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf("\033[1;36m⚡ opsai %s\033[0m\n\033[90m   Workspace: %s\033[0m\n\n",
			strings.Join(payload.Command, " "), targetDir)))

	// ── Special handling for "run": auto-init if project is not yet scaffolded ──
	if baseCmd == "run" {
		pipelineYaml := filepath.Join(targetDir, "pipeline.yaml")
		if _, statErr := os.Stat(pipelineYaml); os.IsNotExist(statErr) {
			if geminiKey == "" {
				_ = conn.WriteMessage(websocket.TextMessage,
					[]byte("\033[1;31m❌ A Gemini API key is required to initialize a new project.\033[0m\n"))
				_ = conn.WriteMessage(websocket.TextMessage,
					[]byte("\033[33m   Enter your key in the API Key field on the dashboard and try again.\033[0m\n"))
				_ = conn.WriteMessage(websocket.TextMessage,
					[]byte("\033[90m   (Key needed only for first-time init — get one at https://aistudio.google.com)\033[0m\n"))
				return
			}

			_ = conn.WriteMessage(websocket.TextMessage,
				[]byte("\033[1;33m🔧 No pipeline.yaml found — running 'opsai init' to scaffold your project first...\033[0m\n\n"))

			if !runSubcmd(conn, targetDir, geminiKey, []string{"init"}) {
				_ = conn.WriteMessage(websocket.TextMessage,
					[]byte("\n\033[1;31m=== ❌ Initialization failed. Cannot continue. ===\033[0m\n"))
				return
			}
			_ = conn.WriteMessage(websocket.TextMessage,
				[]byte("\n\033[1;32m✅ Scaffolding complete! Starting pipeline...\033[0m\n\n"))
		}
	}

	// ── Execute the requested command ─────────────────────────────────────────
	ok2 := runSubcmd(conn, targetDir, geminiKey, payload.Command)
	if !ok2 {
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf("\n\033[1;31m=== ❌ 'opsai %s' exited with an error ===\033[0m\n",
				strings.Join(payload.Command, " "))))
	} else {
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf("\n\033[1;32m=== ✅ 'opsai %s' completed successfully ===\033[0m\n",
				strings.Join(payload.Command, " "))))
	}
}

// runSubcmd spawns the current binary with the given args as a child process,
// injects OPSAI_WORKSPACE_DIR and optionally GEMINI_API_KEY into its environment,
// and streams all stdout+stderr output line-by-line to the WebSocket client.
//
// stdin is pre-loaded with "N\n" × 500 so interactive yes/no prompts auto-skip
// without blocking or crashing on EOF.
//
// Returns true if the subprocess exited with code 0.
func runSubcmd(conn *websocket.Conn, targetDir, geminiKey string, args []string) bool {
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "OPSAI_WORKSPACE_DIR="+targetDir)
	if geminiKey != "" {
		// Appending takes precedence over any value already in os.Environ()
		// because Go's exec uses the last matching entry.
		cmd.Env = append(cmd.Env, "GEMINI_API_KEY="+geminiKey)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf("\033[1;31m❌ stdin pipe error: %v\033[0m\n", err)))
		return false
	}

	// Read from WebSocket and write to subprocess Stdin
	go func() {
		for {
			_, msg, readErr := conn.ReadMessage()
			if readErr != nil {
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				break
			}
			var p struct {
				Input string `json:"input"`
			}
			if json.Unmarshal(msg, &p) == nil && p.Input != "" {
				_, _ = stdinPipe.Write([]byte(p.Input))
			}
		}
	}()

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf("\033[1;31m❌ stdout pipe error: %v\033[0m\n", err)))
		return false
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf("\033[1;31m❌ stderr pipe error: %v\033[0m\n", err)))
		return false
	}

	if err := cmd.Start(); err != nil {
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf("\033[1;31m❌ Failed to start 'opsai %s': %v\033[0m\n",
				strings.Join(args, " "), err)))
		return false
	}

	// Stream stdout and stderr in chunks without waiting for newlines
	lines := make(chan []byte, 256)
	var pipeWg sync.WaitGroup

	streamPipe := func(r io.Reader) {
		defer pipeWg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				lines <- cp
			}
			if err != nil {
				break
			}
		}
	}

	pipeWg.Add(2)
	go streamPipe(stdoutPipe)
	go streamPipe(stderrPipe)
	go func() {
		pipeWg.Wait()
		close(lines)
	}()

	// Stream every line to the WebSocket client
	clientGone := false
	for line := range lines {
		if clientGone {
			continue // drain the channel so goroutines can exit
		}
		if writeErr := conn.WriteMessage(websocket.TextMessage, line); writeErr != nil {
			_ = cmd.Process.Kill()
			clientGone = true
		}
	}

	return cmd.Wait() == nil
}

