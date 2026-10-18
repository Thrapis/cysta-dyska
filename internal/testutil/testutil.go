// Package testutil holds helpers shared by tests in several packages.
package testutil

import (
	"runtime"
	"testing"
)

// RequireWindows skips the test off Windows. Use it for tests that exercise
// Windows paths ("C:\..."), Windows environment variables or the Windows rule
// pack; they have no meaning on macOS.
func RequireWindows(t testing.TB) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
}
