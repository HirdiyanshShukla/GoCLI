//go:build !windows

package terminal

// Stub for non-Windows platforms.
// ANSI colors are typically supported out-of-the-box on Linux/macOS.
func init() {
}
