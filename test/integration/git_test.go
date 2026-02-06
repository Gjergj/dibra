//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

type GitResponse struct {
	Changed          bool   `json:"changed"`
	Failed           bool   `json:"failed"`
	Msg              string `json:"msg,omitempty"`
	Before           string `json:"before,omitempty"`
	After            string `json:"after,omitempty"`
	RemoteURLChanged bool   `json:"remote_url_changed,omitempty"`
}

func getGitResult(t *testing.T, client interface {
	Run(string) (string, string, error)
}, args string) GitResponse {
	t.Helper()
	cmd := `echo '{"module":"git","args":` + args + `}' | /tmp/.dibra-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Logf("Agent stderr: %s", stderr)
	}

	var resp GitResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}
	return resp
}

func setupGitConfig(t *testing.T, client interface {
	Run(string) (string, string, error)
}) {
	t.Helper()
	client.Run("git config --global user.email 'test@example.com'")
	client.Run("git config --global user.name 'Test User'")
	client.Run("git config --global init.defaultBranch main")
	client.Run("git config --global protocol.file.allow always")
}

func createTestRepo(t *testing.T, client interface {
	Run(string) (string, string, error)
}, path string) string {
	t.Helper()
	client.Run("rm -rf " + path)
	client.Run("mkdir -p " + path)
	client.Run("cd " + path + " && git init")
	client.Run("cd " + path + " && echo 'initial content' > file.txt")
	client.Run("cd " + path + " && git add .")
	client.Run("cd " + path + " && git commit -m 'Initial commit'")

	stdout, _, _ := client.Run("cd " + path + " && git rev-parse HEAD")
	return strings.TrimSpace(stdout)
}

func createTestRepoWithBranches(t *testing.T, client interface {
	Run(string) (string, string, error)
}, path string) {
	t.Helper()
	createTestRepo(t, client, path)
	client.Run("cd " + path + " && git checkout -b develop")
	client.Run("cd " + path + " && echo 'develop content' > develop.txt")
	client.Run("cd " + path + " && git add .")
	client.Run("cd " + path + " && git commit -m 'Develop commit'")
	client.Run("cd " + path + " && git checkout main")
}

func createTestRepoWithTags(t *testing.T, client interface {
	Run(string) (string, string, error)
}, path string) {
	t.Helper()
	createTestRepo(t, client, path)
	client.Run("cd " + path + " && git tag v1.0.0")
	client.Run("cd " + path + " && echo 'v1.1 content' > v11.txt")
	client.Run("cd " + path + " && git add .")
	client.Run("cd " + path + " && git commit -m 'v1.1 commit'")
	client.Run("cd " + path + " && git tag v1.1.0")
}

// ==================== Basic Clone Tests ====================

func TestPlaybook_GitCloneBasic(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-test-src"
	destRepo := "/tmp/git-test-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for initial clone")
	}
	if resp.After == "" {
		t.Error("Expected After SHA to be set")
	}
	if !remoteDirExists(t, client, destRepo+"/.git") {
		t.Error("Expected .git directory to exist")
	}
}

func TestPlaybook_GitClonePlaybook(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-playbook-src"
	destRepo := "/tmp/git-playbook-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	playbook := playbookHeader + `
  - name: Clone repository
    git:
      repo: ` + srcRepo + `
      dest: ` + destRepo + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for initial clone")
	}
	if !remoteDirExists(t, client, destRepo+"/.git") {
		t.Error("Expected .git directory to exist")
	}
}

func TestPlaybook_GitCloneIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-idem-src"
	destRepo := "/tmp/git-idem-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp1 := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)
	if resp1.Failed {
		t.Fatalf("First clone failed: %s", resp1.Msg)
	}
	if !resp1.Changed {
		t.Error("First run should change")
	}

	resp2 := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)
	if resp2.Failed {
		t.Fatalf("Second clone failed: %s", resp2.Msg)
	}
	if resp2.Changed {
		t.Error("Second run should not change (idempotent)")
	}
	if resp1.After != resp2.After {
		t.Error("SHA should remain the same")
	}
}

func TestPlaybook_GitCloneNoRepo(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getGitResult(t, client, `{"dest":"/tmp/git-no-repo"}`)

	if !resp.Failed {
		t.Error("Expected failure when repo is missing")
	}
	if !strings.Contains(resp.Msg, "repo is required") {
		t.Errorf("Expected 'repo is required' error, got: %s", resp.Msg)
	}
}

func TestPlaybook_GitCloneNoDest(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getGitResult(t, client, `{"repo":"/tmp/some-repo"}`)

	if !resp.Failed {
		t.Error("Expected failure when dest is missing")
	}
	if !strings.Contains(resp.Msg, "dest is required") {
		t.Errorf("Expected 'dest is required' error, got: %s", resp.Msg)
	}
}

// ==================== Version/Branch Tests ====================

func TestPlaybook_GitCloneBranch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-branch-src"
	destRepo := "/tmp/git-branch-dest"
	createTestRepoWithBranches(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","version":"develop"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for clone")
	}

	branch := remoteExec(t, client, "cd "+destRepo+" && git rev-parse --abbrev-ref HEAD")
	if branch != "develop" {
		t.Errorf("Expected branch 'develop', got: %s", branch)
	}

	if !remoteFileExists(t, client, destRepo+"/develop.txt") {
		t.Error("Expected develop.txt to exist (from develop branch)")
	}
}

func TestPlaybook_GitCloneTag(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-tag-src"
	destRepo := "/tmp/git-tag-dest"
	createTestRepoWithTags(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","version":"v1.0.0"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for clone")
	}

	if remoteFileExists(t, client, destRepo+"/v11.txt") {
		t.Error("v11.txt should NOT exist (v1.0.0 is before v1.1 commit)")
	}
}

func TestPlaybook_GitCheckoutSpecificSHA(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-sha-src"
	destRepo := "/tmp/git-sha-dest"
	initialSHA := createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "cd "+srcRepo+" && echo 'second commit' > second.txt && git add . && git commit -m 'Second'")
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","version":"`+initialSHA+`"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for clone")
	}

	currentSHA := remoteExec(t, client, "cd "+destRepo+" && git rev-parse HEAD")
	if currentSHA != initialSHA {
		t.Errorf("Expected SHA %s, got: %s", initialSHA, currentSHA)
	}
}

func TestPlaybook_GitSwitchBranch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-switch-src"
	destRepo := "/tmp/git-switch-dest"
	createTestRepoWithBranches(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","version":"main"}`)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","version":"develop"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for branch switch")
	}

	branch := remoteExec(t, client, "cd "+destRepo+" && git rev-parse --abbrev-ref HEAD")
	if branch != "develop" {
		t.Errorf("Expected branch 'develop', got: %s", branch)
	}
}

// ==================== Update Tests ====================

func TestPlaybook_GitUpdate(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-update-src"
	destRepo := "/tmp/git-update-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp1 := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)
	if resp1.Failed {
		t.Fatalf("Initial clone failed: %s", resp1.Msg)
	}

	remoteExec(t, client, "cd "+srcRepo+" && echo 'new content' > new.txt && git add . && git commit -m 'New commit'")

	resp2 := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)
	if resp2.Failed {
		t.Fatalf("Update failed: %s", resp2.Msg)
	}
	if !resp2.Changed {
		t.Error("Expected changed=true after source repo update")
	}
	if resp1.After == resp2.After {
		t.Error("SHA should have changed after update")
	}

	if !remoteFileExists(t, client, destRepo+"/new.txt") {
		t.Error("new.txt should exist after update")
	}
}

func TestPlaybook_GitUpdateFalse(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-noupdate-src"
	destRepo := "/tmp/git-noupdate-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp1 := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)
	initialSHA := resp1.After

	remoteExec(t, client, "cd "+srcRepo+" && echo 'new content' > new.txt && git add . && git commit -m 'New commit'")

	resp2 := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","update":false}`)
	if resp2.Failed {
		t.Fatalf("Expected success, got failed: %s", resp2.Msg)
	}
	if resp2.Changed {
		t.Error("Expected changed=false when update=false")
	}

	currentSHA := remoteExec(t, client, "cd "+destRepo+" && git rev-parse HEAD")
	if currentSHA != initialSHA {
		t.Error("SHA should NOT have changed when update=false")
	}
}

// ==================== Force Tests ====================

func TestPlaybook_GitLocalModsWithoutForce(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-mods-src"
	destRepo := "/tmp/git-mods-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)

	remoteExec(t, client, "cd "+destRepo+" && echo 'local change' >> file.txt")

	remoteExec(t, client, "cd "+srcRepo+" && echo 'remote change' > new.txt && git add . && git commit -m 'Remote commit'")

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)
	if !resp.Failed {
		t.Error("Expected failure when local modifications exist without force")
	}
	if !strings.Contains(resp.Msg, "Local modifications") {
		t.Errorf("Expected 'Local modifications' error, got: %s", resp.Msg)
	}
}

func TestPlaybook_GitLocalModsWithForce(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-force-src"
	destRepo := "/tmp/git-force-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)

	remoteExec(t, client, "cd "+destRepo+" && echo 'local change' >> file.txt")

	remoteExec(t, client, "cd "+srcRepo+" && echo 'remote change' > new.txt && git add . && git commit -m 'Remote commit'")

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","force":true}`)
	if resp.Failed {
		t.Fatalf("Expected success with force=true, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true")
	}

	content := remoteFileContent(t, client, destRepo+"/file.txt")
	if strings.Contains(content, "local change") {
		t.Error("Local modifications should have been reset")
	}

	if !remoteFileExists(t, client, destRepo+"/new.txt") {
		t.Error("new.txt should exist after force update")
	}
}

// ==================== Depth (Shallow Clone) Tests ====================

func TestPlaybook_GitShallowClone(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-shallow-src"
	destRepo := "/tmp/git-shallow-dest"
	createTestRepo(t, client, srcRepo)
	for i := 0; i < 5; i++ {
		remoteExec(t, client, "cd "+srcRepo+" && echo 'commit "+string(rune('a'+i))+"' > file"+string(rune('a'+i))+".txt && git add . && git commit -m 'Commit "+string(rune('a'+i))+"'")
	}
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","depth":1}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for clone")
	}

	if !remoteFileExists(t, client, destRepo+"/.git/shallow") {
		t.Error("Expected .git/shallow file for shallow clone")
	}

	commitCount := remoteExec(t, client, "cd "+destRepo+" && git rev-list --count HEAD")
	if commitCount != "1" {
		t.Errorf("Expected 1 commit in shallow clone, got: %s", commitCount)
	}
}

func TestPlaybook_GitShallowClonePlaybook(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-shallow-pb-src"
	destRepo := "/tmp/git-shallow-pb-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "cd "+srcRepo+" && echo 'second' > second.txt && git add . && git commit -m 'Second'")
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	playbook := playbookHeader + `
  - name: Shallow clone
    git:
      repo: ` + srcRepo + `
      dest: ` + destRepo + `
      depth: 1
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for clone")
	}

	if !remoteFileExists(t, client, destRepo+"/.git/shallow") {
		t.Error("Expected shallow clone")
	}
}

// ==================== Bare Repository Tests ====================

func TestPlaybook_GitCloneBare(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-bare-src"
	destRepo := "/tmp/git-bare-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","bare":true}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for clone")
	}

	if !remoteFileExists(t, client, destRepo+"/HEAD") {
		t.Error("Expected HEAD file in bare repo root")
	}
	if remoteFileExists(t, client, destRepo+"/.git") {
		t.Error("Bare repo should not have .git directory")
	}
	if remoteFileExists(t, client, destRepo+"/file.txt") {
		t.Error("Bare repo should not have working tree files")
	}
}

func TestPlaybook_GitCloneBareIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-bare-idem-src"
	destRepo := "/tmp/git-bare-idem-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp1 := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","bare":true}`)
	if resp1.Failed {
		t.Fatalf("First clone failed: %s", resp1.Msg)
	}

	resp2 := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","bare":true}`)
	if resp2.Failed {
		t.Fatalf("Second run failed: %s", resp2.Msg)
	}
	if resp2.Changed {
		t.Error("Second run should not change (idempotent)")
	}
}

// ==================== Remote URL Change Tests ====================

func TestPlaybook_GitChangeRemoteURL(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo1 := "/tmp/git-url1-src"
	srcRepo2 := "/tmp/git-url2-src"
	destRepo := "/tmp/git-url-dest"
	createTestRepo(t, client, srcRepo1)
	createTestRepo(t, client, srcRepo2)
	remoteExec(t, client, "cd "+srcRepo2+" && echo 'repo2 content' > repo2.txt && git add . && git commit -m 'Repo2 commit'")
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo1+" "+srcRepo2+" "+destRepo)

	getGitResult(t, client, `{"repo":"`+srcRepo1+`","dest":"`+destRepo+`"}`)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo2+`","dest":"`+destRepo+`"}`)
	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.RemoteURLChanged {
		t.Error("Expected remote_url_changed=true")
	}
	if !resp.Changed {
		t.Error("Expected changed=true")
	}

	currentURL := remoteExec(t, client, "cd "+destRepo+" && git remote get-url origin")
	if currentURL != srcRepo2 {
		t.Errorf("Expected remote URL %s, got: %s", srcRepo2, currentURL)
	}
}

// ==================== Clone=false Tests ====================

func TestPlaybook_GitCloneFalse(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	destRepo := "/tmp/git-noclone-dest"
	remoteExec(t, client, "rm -rf "+destRepo)

	resp := getGitResult(t, client, `{"repo":"/tmp/nonexistent","dest":"`+destRepo+`","clone":false}`)

	if resp.Failed {
		t.Fatalf("Expected success with clone=false, got failed: %s", resp.Msg)
	}
	if resp.Changed {
		t.Error("Expected changed=false when clone=false and repo doesn't exist")
	}
	if remoteDirExists(t, client, destRepo) {
		t.Error("Repository should not be created when clone=false")
	}
}

func TestPlaybook_GitCloneFalseExistingRepo(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-noclone2-src"
	destRepo := "/tmp/git-noclone2-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)

	remoteExec(t, client, "cd "+srcRepo+" && echo 'new' > new.txt && git add . && git commit -m 'New'")

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","clone":false}`)
	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true when updating existing repo")
	}
}

// ==================== Submodule Tests ====================

func TestPlaybook_GitCloneWithSubmodules(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	submoduleRepo := "/tmp/git-submod-sub"
	mainRepo := "/tmp/git-submod-main"
	destRepo := "/tmp/git-submod-dest"

	createTestRepo(t, client, submoduleRepo)
	remoteExec(t, client, "cd "+submoduleRepo+" && echo 'submodule content' > submod.txt && git add . && git commit -m 'Submodule content'")

	createTestRepo(t, client, mainRepo)
	remoteExec(t, client, "cd "+mainRepo+" && git submodule add "+submoduleRepo+" mysubmodule")
	remoteExec(t, client, "cd "+mainRepo+" && git commit -m 'Add submodule'")

	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+submoduleRepo+" "+mainRepo+" "+destRepo)

	resp := getGitResult(t, client, `{"repo":"`+mainRepo+`","dest":"`+destRepo+`"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true")
	}

	if !remoteDirExists(t, client, destRepo+"/mysubmodule/.git") && !remoteFileExists(t, client, destRepo+"/mysubmodule/.git") {
		t.Error("Expected submodule to be initialized")
	}
	if !remoteFileExists(t, client, destRepo+"/mysubmodule/submod.txt") {
		t.Error("Expected submodule content to exist")
	}
}

func TestPlaybook_GitCloneWithoutSubmodules(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	submoduleRepo := "/tmp/git-nosub-sub"
	mainRepo := "/tmp/git-nosub-main"
	destRepo := "/tmp/git-nosub-dest"

	createTestRepo(t, client, submoduleRepo)
	remoteExec(t, client, "cd "+submoduleRepo+" && echo 'submodule content' > submod.txt && git add . && git commit -m 'Submodule content'")

	createTestRepo(t, client, mainRepo)
	remoteExec(t, client, "cd "+mainRepo+" && git submodule add "+submoduleRepo+" mysubmodule")
	remoteExec(t, client, "cd "+mainRepo+" && git commit -m 'Add submodule'")

	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+submoduleRepo+" "+mainRepo+" "+destRepo)

	resp := getGitResult(t, client, `{"repo":"`+mainRepo+`","dest":"`+destRepo+`","recursive":false}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	if remoteFileExists(t, client, destRepo+"/mysubmodule/submod.txt") {
		t.Error("Submodule content should NOT exist when recursive=false")
	}
}

// ==================== Single Branch Tests ====================

func TestPlaybook_GitSingleBranch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-single-src"
	destRepo := "/tmp/git-single-dest"
	createTestRepoWithBranches(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","version":"main","single_branch":true}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true")
	}

	refs := remoteExec(t, client, "cd "+destRepo+" && git branch -r")
	if strings.Contains(refs, "develop") {
		t.Error("develop branch should not be fetched with single_branch=true")
	}
}

// ==================== Separate Git Dir Tests ====================

func TestPlaybook_GitSeparateGitDir(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-sep-src"
	destRepo := "/tmp/git-sep-dest"
	gitDir := "/tmp/git-sep-gitdir"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo+" "+gitDir)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo+" "+gitDir)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","separate_git_dir":"`+gitDir+`"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true")
	}

	if !remoteDirExists(t, client, gitDir) {
		t.Error("Expected separate git dir to exist")
	}
	if remoteDirExists(t, client, destRepo+"/.git") {
		gitContent := remoteExec(t, client, "cat "+destRepo+"/.git 2>/dev/null || echo 'dir'")
		if !strings.Contains(gitContent, "gitdir:") && gitContent == "dir" {
			t.Error(".git should be a file pointing to separate git dir, not a directory")
		}
	}

	if !remoteFileExists(t, client, destRepo+"/file.txt") {
		t.Error("Working tree files should exist")
	}
}

// ==================== Playbook Integration Tests ====================

func TestPlaybook_GitFullWorkflow(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-workflow-src"
	destRepo := "/tmp/git-workflow-dest"
	createTestRepoWithBranches(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	playbook := playbookHeader + `
  - name: Clone repository to main
    git:
      repo: ` + srcRepo + `
      dest: ` + destRepo + `
      version: main
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Clone failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for initial clone")
	}

	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Second run should not change (idempotent)")
	}

	playbook2 := playbookHeader + `
  - name: Switch to develop
    git:
      repo: ` + srcRepo + `
      dest: ` + destRepo + `
      version: develop
`
	output3 := runPlaybook(t, playbook2)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Switch failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED for branch switch")
	}

	branch := remoteExec(t, client, "cd "+destRepo+" && git rev-parse --abbrev-ref HEAD")
	if branch != "develop" {
		t.Errorf("Expected branch 'develop', got: %s", branch)
	}
}

func TestPlaybook_GitMultipleRepos(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	setupGitConfig(t, client)
	srcRepo1 := "/tmp/git-multi-src1"
	srcRepo2 := "/tmp/git-multi-src2"
	destRepo1 := "/tmp/git-multi-dest1"
	destRepo2 := "/tmp/git-multi-dest2"
	createTestRepo(t, client, srcRepo1)
	createTestRepo(t, client, srcRepo2)
	remoteExec(t, client, "rm -rf "+destRepo1+" "+destRepo2)
	defer remoteExec(t, client, "rm -rf "+srcRepo1+" "+srcRepo2+" "+destRepo1+" "+destRepo2)

	playbook := playbookHeader + `
  - name: Clone first repo
    git:
      repo: ` + srcRepo1 + `
      dest: ` + destRepo1 + `

  - name: Clone second repo
    git:
      repo: ` + srcRepo2 + `
      dest: ` + destRepo2 + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}

	changedCount := strings.Count(output, "CHANGED")
	if changedCount != 2 {
		t.Errorf("Expected 2 CHANGED, got: %d", changedCount)
	}

	if !remoteDirExists(t, client, destRepo1+"/.git") {
		t.Error("First repo should be cloned")
	}
	if !remoteDirExists(t, client, destRepo2+"/.git") {
		t.Error("Second repo should be cloned")
	}
}

// ==================== Error Handling Tests ====================

func TestPlaybook_GitInvalidRepo(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getGitResult(t, client, `{"repo":"/nonexistent/path","dest":"/tmp/git-invalid-dest"}`)

	if !resp.Failed {
		t.Error("Expected failure for invalid repo")
	}
}

func TestPlaybook_GitVersionNotFound(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-notfound-src"
	destRepo := "/tmp/git-notfound-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`","version":"nonexistent-branch"}`)

	if !resp.Failed {
		t.Error("Expected failure for nonexistent version")
	}
}

// ==================== Before/After SHA Tests ====================

func TestPlaybook_GitBeforeAfterSHA(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	setupGitConfig(t, client)
	srcRepo := "/tmp/git-sha-test-src"
	destRepo := "/tmp/git-sha-test-dest"
	createTestRepo(t, client, srcRepo)
	remoteExec(t, client, "rm -rf "+destRepo)
	defer remoteExec(t, client, "rm -rf "+srcRepo+" "+destRepo)

	resp1 := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)
	if resp1.Failed {
		t.Fatalf("Clone failed: %s", resp1.Msg)
	}
	if resp1.Before != "" {
		t.Error("Before should be empty for initial clone")
	}
	if resp1.After == "" {
		t.Error("After should be set after clone")
	}
	if len(resp1.After) != 40 {
		t.Errorf("After should be a 40-char SHA, got: %s", resp1.After)
	}

	remoteExec(t, client, "cd "+srcRepo+" && echo 'update' > update.txt && git add . && git commit -m 'Update'")

	resp2 := getGitResult(t, client, `{"repo":"`+srcRepo+`","dest":"`+destRepo+`"}`)
	if resp2.Failed {
		t.Fatalf("Update failed: %s", resp2.Msg)
	}
	if resp2.Before != resp1.After {
		t.Errorf("Before should equal previous After. Before: %s, Expected: %s", resp2.Before, resp1.After)
	}
	if resp2.After == resp2.Before {
		t.Error("After should be different from Before after update")
	}
}
