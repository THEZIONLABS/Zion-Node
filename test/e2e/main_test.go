package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain runs before/after all tests in the e2e package.
// It cleans up any stale temp directories left by previous test runs.
func TestMain(m *testing.M) {
	// Pre-cleanup: remove stale temp dirs from prior crashed/interrupted runs
	cleanStaleTempDirs()

	code := m.Run()

	// Post-cleanup: remove any dirs that leaked due to race conditions
	cleanStaleTempDirs()

	os.Exit(code)
}

// cleanStaleTempDirs removes orphaned zion-node test directories from /tmp.
func cleanStaleTempDirs() {
	patterns := []string{
		"/tmp/zion-node-test-*",
		"/tmp/zion-node-real-e2e-*",
		"/tmp/zion-node-full-e2e-*",
		"/tmp/zion-e2e-home-*",
		"/tmp/zion-e2e-extended-*",
		"/tmp/zion-e2e-recovery-*",
		"/tmp/zion-e2e-restart-*",
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			os.RemoveAll(m)
		}
	}
}
