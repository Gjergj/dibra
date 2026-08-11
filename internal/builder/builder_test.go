package builder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceHashIncludesEveryLocalAgentDependency(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"cmd/agent/main.go":                 "package main\n",
		"internal/execution/request.go":     "package execution\n",
		"internal/modules/ping/ping.go":     "package ping\n",
		"internal/version/version.go":       "package version\n",
		"internal/execution/result_test.go": "package execution\n",
		"go.mod":                            "module example.test/dibra\n",
		"go.sum":                            "",
	}
	for name, contents := range files {
		writeBuilderTestFile(t, root, name, contents)
	}

	builder := New(root)
	initial, err := builder.sourceHash()
	if err != nil {
		t.Fatal(err)
	}

	writeBuilderTestFile(t, root, "internal/execution/request.go", "package execution\n\nconst changed = true\n")
	sharedDependencyChanged, err := builder.sourceHash()
	if err != nil {
		t.Fatal(err)
	}
	if sharedDependencyChanged == initial {
		t.Fatal("execution source change did not invalidate the agent cache key")
	}

	writeBuilderTestFile(t, root, "internal/execution/result_test.go", "package execution\n\nfunc ignored() {}\n")
	testOnlyChanged, err := builder.sourceHash()
	if err != nil {
		t.Fatal(err)
	}
	if testOnlyChanged != sharedDependencyChanged {
		t.Fatal("test-only source change unexpectedly invalidated the agent cache key")
	}

	writeBuilderTestFile(t, root, "go.mod", "module example.test/dibra\n\ngo 1.25\n")
	moduleChanged, err := builder.sourceHash()
	if err != nil {
		t.Fatal(err)
	}
	if moduleChanged == testOnlyChanged {
		t.Fatal("go.mod change did not invalidate the agent cache key")
	}
}

func writeBuilderTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
