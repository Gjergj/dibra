package git

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteRejectsLocalCommitsWithoutForce(t *testing.T) {
	source := newTestRepository(t)
	dest := filepath.Join(t.TempDir(), "checkout")

	cloneTestRepository(t, source, dest)
	writeTestFile(t, dest, "local.txt", "local commit")
	runTestGit(t, dest, "add", "local.txt")
	runTestGit(t, dest, "-c", "user.name=Dibra Test", "-c", "user.email=dibra-test@example.com", "commit", "-m", "local commit")

	localHead := getVersion(dest, "HEAD", "git", false)
	resp := Execute(Request{Repo: source, Dest: dest})

	if !resp.Failed {
		t.Fatal("expected update to fail when it would discard local commits")
	}
	if !strings.Contains(resp.Msg, "local commits would be discarded") {
		t.Fatalf("expected local-commit safety error, got: %s", resp.Msg)
	}
	if got := getVersion(dest, "HEAD", "git", false); got != localHead {
		t.Fatalf("local commit was changed after rejected update: got %s, want %s", got, localHead)
	}
	if _, err := os.Stat(filepath.Join(dest, "local.txt")); err != nil {
		t.Fatalf("local commit contents were lost after rejected update: %v", err)
	}
}

func TestExecuteRejectsDivergedBranchWithoutForce(t *testing.T) {
	source := newTestRepository(t)
	dest := filepath.Join(t.TempDir(), "checkout")

	cloneTestRepository(t, source, dest)
	writeTestFile(t, dest, "local.txt", "local commit")
	runTestGit(t, dest, "add", "local.txt")
	runTestGit(t, dest, "-c", "user.name=Dibra Test", "-c", "user.email=dibra-test@example.com", "commit", "-m", "local commit")
	localHead := getVersion(dest, "HEAD", "git", false)

	writeTestFile(t, source, "remote.txt", "remote commit")
	runTestGit(t, source, "add", "remote.txt")
	runTestGit(t, source, "-c", "user.name=Dibra Test", "-c", "user.email=dibra-test@example.com", "commit", "-m", "remote commit")

	resp := Execute(Request{Repo: source, Dest: dest, Version: "main"})

	if !resp.Failed {
		t.Fatal("expected diverged update to fail without force")
	}
	if !strings.Contains(resp.Msg, "local commits would be discarded") {
		t.Fatalf("expected local-commit safety error, got: %s", resp.Msg)
	}
	if got := getVersion(dest, "HEAD", "git", false); got != localHead {
		t.Fatalf("local commit was changed after rejected divergent update: got %s, want %s", got, localHead)
	}
	if _, err := os.Stat(filepath.Join(dest, "local.txt")); err != nil {
		t.Fatalf("local commit contents were lost after rejected divergent update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "remote.txt")); !os.IsNotExist(err) {
		t.Fatalf("remote commit was applied despite rejected divergent update")
	}
}

func TestExecuteForceDiscardsLocalCommits(t *testing.T) {
	source := newTestRepository(t)
	dest := filepath.Join(t.TempDir(), "checkout")

	cloneTestRepository(t, source, dest)
	writeTestFile(t, dest, "local.txt", "local commit")
	runTestGit(t, dest, "add", "local.txt")
	runTestGit(t, dest, "-c", "user.name=Dibra Test", "-c", "user.email=dibra-test@example.com", "commit", "-m", "local commit")

	writeTestFile(t, source, "remote.txt", "remote commit")
	runTestGit(t, source, "add", "remote.txt")
	runTestGit(t, source, "-c", "user.name=Dibra Test", "-c", "user.email=dibra-test@example.com", "commit", "-m", "remote commit")
	remoteHead := getVersion(source, "HEAD", "git", false)

	resp := Execute(Request{Repo: source, Dest: dest, Force: true})

	if resp.Failed {
		t.Fatalf("expected force update to succeed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Fatal("expected force update to report changed=true")
	}
	if got := getVersion(dest, "HEAD", "git", false); got != remoteHead {
		t.Fatalf("force update did not reset to remote: got %s, want %s", got, remoteHead)
	}
	if _, err := os.Stat(filepath.Join(dest, "local.txt")); !os.IsNotExist(err) {
		t.Fatalf("local commit contents were not discarded with force=true")
	}
	if _, err := os.Stat(filepath.Join(dest, "remote.txt")); err != nil {
		t.Fatalf("remote commit contents were not applied with force=true: %v", err)
	}
}

func newTestRepository(t *testing.T) string {
	t.Helper()

	repo := filepath.Join(t.TempDir(), "repository")
	runTestGit(t, "", "init", repo)
	runTestGit(t, repo, "symbolic-ref", "HEAD", "refs/heads/main")
	writeTestFile(t, repo, "initial.txt", "initial commit")
	runTestGit(t, repo, "add", "initial.txt")
	runTestGit(t, repo, "-c", "user.name=Dibra Test", "-c", "user.email=dibra-test@example.com", "commit", "-m", "initial commit")
	return repo
}

func cloneTestRepository(t *testing.T, source, dest string) {
	t.Helper()

	resp := Execute(Request{Repo: source, Dest: dest})
	if resp.Failed {
		t.Fatalf("test repository clone failed: %s", resp.Msg)
	}
}

func writeTestFile(t *testing.T, repo, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(repo, name), []byte(content+"\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}
