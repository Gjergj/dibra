package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareLocalInstallsAndReusesVersionedAgent(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	source := filepath.Join(temporary, "source-agent")
	writeVersionAgent(t, source, "dev", "first")
	runtimePath := filepath.Join(temporary, "runtime", "dibra-agent")
	resolver := NewResolver(Options{Mode: ModePath, AgentPath: source, Version: "dev"})

	installed, err := resolver.PrepareLocal(Target{OS: "linux", Arch: "amd64"}, runtimePath, "dev", false)
	if err != nil {
		t.Fatalf("PrepareLocal() error = %v", err)
	}
	if installed != runtimePath {
		t.Fatalf("installed path = %q", installed)
	}
	before, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	writeVersionAgent(t, source, "dev", "second")
	if _, err := resolver.PrepareLocal(Target{OS: "linux", Arch: "amd64"}, runtimePath, "dev", false); err != nil {
		t.Fatalf("reuse PrepareLocal() error = %v", err)
	}
	afterReuse, _ := os.ReadFile(runtimePath)
	if string(afterReuse) != string(before) {
		t.Fatal("runtime agent changed without force")
	}
	if _, err := resolver.PrepareLocal(Target{OS: "linux", Arch: "amd64"}, runtimePath, "dev", true); err != nil {
		t.Fatalf("forced PrepareLocal() error = %v", err)
	}
	afterForce, _ := os.ReadFile(runtimePath)
	if string(afterForce) == string(before) {
		t.Fatal("runtime agent did not change when forced")
	}
}

func TestPrepareLocalRejectsMismatchedExplicitAgent(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	source := filepath.Join(temporary, "source-agent")
	writeVersionAgent(t, source, "v1.0.0", "")
	resolver := NewResolver(Options{Mode: ModePath, AgentPath: source, Version: "v2.0.0"})
	if _, err := resolver.PrepareLocal(Target{}, filepath.Join(temporary, "runtime-agent"), "v2.0.0", false); err == nil {
		t.Fatal("PrepareLocal() succeeded with a mismatched explicit agent")
	}
}

func writeVersionAgent(t *testing.T, path, version, marker string) {
	t.Helper()
	script := "#!/bin/sh\necho 'dibra-agent " + version + " (commit: none, built: unknown)'\n# " + marker + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
