package git

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func Execute(req Request) Response {
	resp := Response{
		Changed: false,
	}

	if req.Repo == "" {
		resp.Failed = true
		resp.Msg = "repo is required"
		return resp
	}

	clone := true
	if req.Clone != nil {
		clone = *req.Clone
	}

	if clone && req.Dest == "" {
		resp.Failed = true
		resp.Msg = "dest is required when clone=true"
		return resp
	}

	gitExec := "git"
	if req.Executable != "" {
		gitExec = req.Executable
	}

	if _, err := exec.LookPath(gitExec); err != nil {
		resp.Failed = true
		resp.Msg = "git executable not found: " + gitExec
		return resp
	}

	remote := req.Remote
	if remote == "" {
		remote = "origin"
	}

	version := req.Version
	if version == "" {
		version = "HEAD"
	}

	update := true
	if req.Update != nil {
		update = *req.Update
	}

	recursive := true
	if req.Recursive != nil {
		recursive = *req.Recursive
	}

	repoExists := isRepoAt(req.Dest, req.Bare)

	if repoExists {
		resp.Before = getVersion(req.Dest, "HEAD", gitExec, req.Bare)

		currentRemoteURL := getRemoteURL(req.Dest, remote, gitExec)
		if currentRemoteURL != "" && currentRemoteURL != req.Repo {
			if err := setRemoteURL(req.Dest, remote, req.Repo, gitExec); err != nil {
				resp.Failed = true
				resp.Msg = "failed to set remote URL: " + err.Error()
				return resp
			}
			resp.RemoteURLChanged = true
			resp.Changed = true
		}

		if update {
			if hasLocalMods(req.Dest, gitExec) {
				if !req.Force {
					resp.Failed = true
					resp.Msg = "Local modifications exist in repository. Use force=true to discard them."
					return resp
				}
				if err := resetHard(req.Dest, gitExec); err != nil {
					resp.Failed = true
					resp.Msg = "failed to reset: " + err.Error()
					return resp
				}
				resp.Changed = true
			}

			if err := fetchRepo(req, gitExec, remote); err != nil {
				resp.Failed = true
				resp.Msg = "failed to fetch: " + err.Error()
				return resp
			}

			if err := switchVersion(req, gitExec, remote, version); err != nil {
				resp.Failed = true
				resp.Msg = "failed to switch version: " + err.Error()
				return resp
			}

			if recursive && !req.Bare {
				if err := updateSubmodules(req.Dest, recursive, req.TrackSubmodules, gitExec); err != nil {
					resp.Failed = true
					resp.Msg = "failed to update submodules: " + err.Error()
					return resp
				}
			}
		}

		resp.After = getVersion(req.Dest, "HEAD", gitExec, req.Bare)
		if resp.Before != resp.After {
			resp.Changed = true
		}
	} else if clone {
		if err := cloneRepo(req, gitExec, version); err != nil {
			resp.Failed = true
			resp.Msg = "failed to clone: " + err.Error()
			return resp
		}
		resp.Changed = true

		if recursive && !req.Bare {
			if err := updateSubmodules(req.Dest, recursive, req.TrackSubmodules, gitExec); err != nil {
				resp.Failed = true
				resp.Msg = "failed to update submodules: " + err.Error()
				return resp
			}
		}

		resp.After = getVersion(req.Dest, "HEAD", gitExec, req.Bare)
	} else {
		resp.Msg = "repository does not exist and clone=false"
	}

	return resp
}

func isRepoAt(dest string, bare bool) bool {
	if dest == "" {
		return false
	}
	if bare {
		headPath := filepath.Join(dest, "HEAD")
		if _, err := os.Stat(headPath); err == nil {
			return true
		}
		return false
	}
	gitDir := filepath.Join(dest, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func cloneRepo(req Request, gitExec, version string) error {
	args := []string{"clone"}

	if req.Bare {
		args = append(args, "--bare")
	}

	if req.Depth != nil && *req.Depth > 0 {
		if !isSHA(version) {
			args = append(args, "--depth", intToStr(*req.Depth))
		}
	}

	if req.SingleBranch {
		args = append(args, "--single-branch")
	}

	recursive := true
	if req.Recursive != nil {
		recursive = *req.Recursive
	}
	if recursive && !req.Bare {
		args = append(args, "--recursive")
	}

	if version != "HEAD" && !isSHA(version) {
		args = append(args, "--branch", version)
	}

	if req.SeparateGitDir != "" {
		args = append(args, "--separate-git-dir", req.SeparateGitDir)
	}

	// For local paths with depth specified, use file:// protocol to enable shallow clone
	repo := req.Repo
	if req.Depth != nil && *req.Depth > 0 && isLocalPath(repo) {
		repo = "file://" + repo
	}

	args = append(args, repo, req.Dest)

	cmd := exec.Command(gitExec, args...)
	cmd.Env = buildSSHEnv(req)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return wrapError(err, stderr.String())
	}

	if version != "HEAD" && isSHA(version) {
		return checkoutVersion(req.Dest, version, gitExec)
	}

	return nil
}

func fetchRepo(req Request, gitExec, remote string) error {
	args := []string{"fetch", remote}

	if req.Force {
		args = append(args, "--force")
	}

	if req.Depth != nil && *req.Depth > 0 {
		args = append(args, "--depth", intToStr(*req.Depth))
	}

	args = append(args, "--tags")

	if req.Refspec != "" {
		args = append(args, req.Refspec)
	}

	cmd := exec.Command(gitExec, args...)
	cmd.Dir = req.Dest
	cmd.Env = buildSSHEnv(req)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return wrapError(err, stderr.String())
	}

	return nil
}

func switchVersion(req Request, gitExec, remote, version string) error {
	if req.Bare {
		return nil
	}

	if version == "HEAD" {
		return resetToRemoteHEAD(req.Dest, remote, gitExec)
	}

	if isSHA(version) {
		return checkoutVersion(req.Dest, version, gitExec)
	}

	if isRemoteBranch(req.Dest, remote, version, gitExec) {
		localBranchExists := isLocalBranch(req.Dest, version, gitExec)
		remoteBranchExists := hasRemoteRef(req.Dest, remote, version, gitExec)

		if !remoteBranchExists {
			if err := fetchBranch(req, gitExec, remote, version); err != nil {
				return err
			}
		}

		if localBranchExists {
			if err := checkoutVersion(req.Dest, version, gitExec); err != nil {
				return err
			}
			return resetToRemoteBranch(req.Dest, remote, version, gitExec)
		}

		return checkoutTrackingBranch(req.Dest, remote, version, gitExec)
	}

	if isRemoteTag(req.Dest, remote, version, gitExec) {
		return checkoutVersion(req.Dest, version, gitExec)
	}

	return checkoutVersion(req.Dest, version, gitExec)
}

func resetToRemoteHEAD(dest, remote, gitExec string) error {
	branch := getCurrentBranch(dest, gitExec)
	if branch == "" {
		return nil
	}
	return resetToRemoteBranch(dest, remote, branch, gitExec)
}

func resetToRemoteBranch(dest, remote, branch, gitExec string) error {
	remoteBranch := remote + "/" + branch
	cmd := exec.Command(gitExec, "reset", "--hard", remoteBranch)
	cmd.Dir = dest
	cmd.Env = os.Environ()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return wrapError(err, stderr.String())
	}
	return nil
}

func getCurrentBranch(dest, gitExec string) string {
	cmd := exec.Command(gitExec, "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dest

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return ""
	}
	branch := strings.TrimSpace(stdout.String())
	if branch == "HEAD" {
		return ""
	}
	return branch
}

func checkoutVersion(dest, version, gitExec string) error {
	cmd := exec.Command(gitExec, "checkout", version)
	cmd.Dir = dest

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return wrapError(err, stderr.String())
	}
	return nil
}

func checkoutTrackingBranch(dest, remote, branch, gitExec string) error {
	cmd := exec.Command(gitExec, "checkout", "-b", branch, "--track", remote+"/"+branch)
	cmd.Dir = dest

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return wrapError(err, stderr.String())
	}
	return nil
}

func getVersion(dest, ref, gitExec string, bare bool) string {
	cmd := exec.Command(gitExec, "rev-parse", ref)
	if !bare {
		cmd.Dir = dest
	} else {
		cmd.Args = append(cmd.Args[:1], append([]string{"--git-dir", dest}, cmd.Args[1:]...)...)
	}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

func hasLocalMods(dest, gitExec string) bool {
	cmd := exec.Command(gitExec, "status", "--porcelain")
	cmd.Dir = dest

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return false
	}
	return strings.TrimSpace(stdout.String()) != ""
}

func isRemoteBranch(dest, remote, version, gitExec string) bool {
	cmd := exec.Command(gitExec, "ls-remote", "--heads", remote, version)
	cmd.Dir = dest

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return false
	}
	return strings.TrimSpace(stdout.String()) != ""
}

func isRemoteTag(dest, remote, version, gitExec string) bool {
	cmd := exec.Command(gitExec, "ls-remote", "--tags", remote, version)
	cmd.Dir = dest

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return false
	}
	return strings.TrimSpace(stdout.String()) != ""
}

func isLocalBranch(dest, branch, gitExec string) bool {
	cmd := exec.Command(gitExec, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = dest
	return cmd.Run() == nil
}

func hasRemoteRef(dest, remote, branch, gitExec string) bool {
	cmd := exec.Command(gitExec, "show-ref", "--verify", "--quiet", "refs/remotes/"+remote+"/"+branch)
	cmd.Dir = dest
	return cmd.Run() == nil
}

func fetchBranch(req Request, gitExec, remote, branch string) error {
	args := []string{"fetch", remote, branch + ":" + "refs/remotes/" + remote + "/" + branch}

	if req.Force {
		args = append(args, "--force")
	}

	cmd := exec.Command(gitExec, args...)
	cmd.Dir = req.Dest
	cmd.Env = buildSSHEnv(req)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return wrapError(err, stderr.String())
	}
	return nil
}

var shaRegex = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

func isSHA(version string) bool {
	return shaRegex.MatchString(version)
}

// isLocalPath returns true if the path is a local filesystem path (not a URL)
func isLocalPath(path string) bool {
	if strings.HasPrefix(path, "/") {
		return true
	}
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") {
		return true
	}
	// Check for URL schemes
	if strings.Contains(path, "://") {
		return false
	}
	// Check for SSH-style URLs (git@host:path)
	if strings.Contains(path, "@") && strings.Contains(path, ":") {
		return false
	}
	return true
}

func resetHard(dest, gitExec string) error {
	cmd := exec.Command(gitExec, "reset", "--hard", "HEAD")
	cmd.Dir = dest

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return wrapError(err, stderr.String())
	}

	cleanCmd := exec.Command(gitExec, "clean", "-fd")
	cleanCmd.Dir = dest
	_ = cleanCmd.Run()

	return nil
}

func getRemoteURL(dest, remote, gitExec string) string {
	cmd := exec.Command(gitExec, "remote", "get-url", remote)
	cmd.Dir = dest

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

func setRemoteURL(dest, remote, url, gitExec string) error {
	cmd := exec.Command(gitExec, "remote", "set-url", remote, url)
	cmd.Dir = dest

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return wrapError(err, stderr.String())
	}
	return nil
}

func updateSubmodules(dest string, recursive, track bool, gitExec string) error {
	args := []string{"submodule", "update", "--init"}

	if recursive {
		args = append(args, "--recursive")
	}

	if track {
		args = append(args, "--remote")
	}

	cmd := exec.Command(gitExec, args...)
	cmd.Dir = dest

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return wrapError(err, stderr.String())
	}
	return nil
}

func buildSSHEnv(req Request) []string {
	env := os.Environ()

	var sshOpts []string

	sshOpts = append(sshOpts, "-o", "BatchMode=yes")

	if req.AcceptHostkey {
		sshOpts = append(sshOpts, "-o", "StrictHostKeyChecking=no")
	} else if req.AcceptNewhostkey {
		sshOpts = append(sshOpts, "-o", "StrictHostKeyChecking=accept-new")
	}

	if req.KeyFile != "" {
		sshOpts = append(sshOpts, "-i", req.KeyFile, "-o", "IdentitiesOnly=yes")
	}

	if req.SSHOpts != "" {
		extraOpts := strings.Fields(req.SSHOpts)
		sshOpts = append(sshOpts, extraOpts...)
	}

	if len(sshOpts) > 0 {
		sshCmd := "ssh " + strings.Join(sshOpts, " ")
		env = append(env, "GIT_SSH_COMMAND="+sshCmd)
	}

	return env
}

func intToStr(n int) string {
	return strconv.Itoa(n)
}

func wrapError(err error, stderr string) error {
	if stderr != "" {
		return &gitError{msg: err.Error() + ": " + strings.TrimSpace(stderr)}
	}
	return err
}

type gitError struct {
	msg string
}

func (e *gitError) Error() string {
	return e.msg
}
