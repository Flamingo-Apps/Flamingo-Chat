//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	adminBinaryOnce sync.Once
	adminBinaryPath string
	adminBinaryErr  error
)

// buildAdminTool builds tools/admin once per test run and returns the
// path to the binary - a real subprocess exec, not a direct call into
// its internal package (which Go's own internal-package visibility
// rules wouldn't allow from this separate module anyway), so this
// exercises the actual deployable artifact docs/ARCHITECTURE.md §11
// means by "through the admin tool".
func buildAdminTool(t *testing.T) string {
	t.Helper()
	adminBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "flamingo-admin-e2e")
		if err != nil {
			adminBinaryErr = err
			return
		}
		adminBinaryPath = filepath.Join(dir, "admin")
		//nolint:gosec // adminBinaryPath is this test's own os.MkdirTemp path, not external input
		cmd := exec.Command("go", "build", "-o", adminBinaryPath, "../../tools/admin")
		if out, err := cmd.CombinedOutput(); err != nil {
			adminBinaryErr = err
			t.Logf("go build tools/admin output:\n%s", out)
		}
	})
	if adminBinaryErr != nil {
		t.Fatalf("build tools/admin: %v", adminBinaryErr)
	}
	return adminBinaryPath
}

// runAdmin runs tools/admin with args against moderationAddr (default
// localhost:50055, Tilt's own port-forward for Moderation), returning
// its combined stdout+stderr. It fails the test if the command itself
// fails to start or exits non-zero - a smoke test's job is to assert on
// successful admin actions, same as every other flow here.
func runAdmin(t *testing.T, args ...string) string {
	t.Helper()
	binary := buildAdminTool(t)
	addr := os.Getenv("MODERATION_ADDR")
	if addr == "" {
		addr = "localhost:50055"
	}
	//nolint:gosec // binary is the path this same test just built; args come from this test's own fixed call sites, not external input
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "MODERATION_ADDR="+addr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("admin %v: %v\noutput:\n%s", args, err, out)
	}
	return string(out)
}
