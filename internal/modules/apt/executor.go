package apt

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	aptGetCmd    = "/usr/bin/apt-get"
	dpkgQueryCmd = "/usr/bin/dpkg-query"

	aptListsPath              = "/var/lib/apt/lists"
	aptUpdateSuccessStampPath = "/var/lib/apt/periodic/update-success-stamp"
	aptPkgCachePath           = "/var/cache/apt/pkgcache.bin"
)

var envVars = []string{
	"DEBIAN_FRONTEND=noninteractive",
	"DEBIAN_PRIORITY=critical",
	"LANG=C.UTF-8",
	"LC_ALL=C.UTF-8",
}

func Execute(req Request) Response {
	switch {
	case req.UpdateCache:
		resp := updateCache(req.CacheValidTime)
		if resp.Failed {
			return resp
		}
		if len(req.Packages) == 0 && req.Upgrade == "" {
			return resp
		}
		if len(req.Packages) > 0 {
			pkgResp := handlePackages(req)
			pkgResp.Changed = pkgResp.Changed || resp.Changed
			return pkgResp
		}
		if req.Upgrade != "" {
			upgResp := handleUpgrade(req.Upgrade)
			upgResp.Changed = upgResp.Changed || resp.Changed
			return upgResp
		}
		return resp

	case len(req.Packages) > 0:
		return handlePackages(req)

	case req.Upgrade != "":
		return handleUpgrade(req.Upgrade)

	case req.Autoremove:
		return autoremove(req.Purge)

	default:
		return Response{Failed: true, Msg: "no action specified"}
	}
}

func updateCache(cacheValidTime int) Response {
	if cacheValidTime > 0 && aptCacheIsFresh(cacheValidTime) {
		return Response{Changed: false, Msg: "cache is still valid"}
	}

	rc, stdout, stderr := runAptGet("update")
	if rc != 0 {
		return Response{
			Failed: true,
			RC:     rc,
			Stdout: stdout,
			Stderr: stderr,
			Msg:    "failed to update cache",
		}
	}
	_ = touchFile(aptUpdateSuccessStampPath)

	return Response{Changed: true, RC: rc, Stdout: stdout, Stderr: stderr}
}

func touchFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	now := time.Now()
	return os.Chtimes(path, now, now)
}

func aptCacheIsFresh(validSeconds int) bool {
	mtime, ok := newestAptCacheMtime(aptUpdateSuccessStampPath, aptPkgCachePath, aptListsPath)
	if !ok {
		return false
	}
	return time.Since(mtime) < time.Duration(validSeconds)*time.Second
}

func newestAptCacheMtime(stampPath, pkgCachePath, listsDir string) (time.Time, bool) {
	var newest time.Time
	found := false
	consider := func(info os.FileInfo) {
		if info == nil {
			return
		}
		if !found || info.ModTime().After(newest) {
			newest = info.ModTime()
			found = true
		}
	}

	for _, path := range []string{stampPath, pkgCachePath, listsDir} {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err == nil {
			consider(info)
		}
	}
	if listsDir == "" {
		return newest, found
	}
	entries, err := os.ReadDir(listsDir)
	if err != nil {
		return newest, found
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "lock" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		consider(info)
	}
	return newest, found
}

func handlePackages(req Request) Response {
	switch req.State {
	case "present", "installed":
		return installPackages(req.Packages)
	case "absent", "removed":
		return removePackages(req.Packages, req.Purge)
	case "latest":
		return upgradePackages(req.Packages)
	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", req.State)}
	}
}

func installPackages(packages []string) Response {
	toInstall := []string{}
	for _, pkg := range packages {
		if !isInstalled(pkg) {
			toInstall = append(toInstall, pkg)
		}
	}

	if len(toInstall) == 0 {
		return Response{Changed: false, Msg: "all packages already installed"}
	}

	args := append([]string{"-y", "install"}, toInstall...)
	rc, stdout, stderr := runAptGet(args...)

	if rc != 0 {
		return Response{
			Failed:   true,
			RC:       rc,
			Stdout:   stdout,
			Stderr:   stderr,
			Msg:      "failed to install packages",
			Packages: toInstall,
		}
	}

	return Response{
		Changed:  true,
		RC:       rc,
		Stdout:   stdout,
		Stderr:   stderr,
		Packages: toInstall,
	}
}

func removePackages(packages []string, purge bool) Response {
	toRemove := []string{}
	for _, pkg := range packages {
		if isInstalled(pkg) {
			toRemove = append(toRemove, pkg)
		}
	}

	if len(toRemove) == 0 {
		return Response{Changed: false, Msg: "no packages to remove"}
	}

	action := "remove"
	if purge {
		action = "purge"
	}

	args := append([]string{"-y", action}, toRemove...)
	rc, stdout, stderr := runAptGet(args...)

	if rc != 0 {
		return Response{
			Failed:   true,
			RC:       rc,
			Stdout:   stdout,
			Stderr:   stderr,
			Msg:      "failed to remove packages",
			Packages: toRemove,
		}
	}

	return Response{
		Changed:  true,
		RC:       rc,
		Stdout:   stdout,
		Stderr:   stderr,
		Packages: toRemove,
	}
}

func upgradePackages(packages []string) Response {
	args := append([]string{"-y", "install", "--only-upgrade"}, packages...)
	rc, stdout, stderr := runAptGet(args...)

	changed := !strings.Contains(stdout, "0 upgraded")

	if rc != 0 {
		return Response{
			Failed:   true,
			RC:       rc,
			Stdout:   stdout,
			Stderr:   stderr,
			Msg:      "failed to upgrade packages",
			Packages: packages,
		}
	}

	return Response{
		Changed:  changed,
		RC:       rc,
		Stdout:   stdout,
		Stderr:   stderr,
		Packages: packages,
	}
}

func handleUpgrade(mode string) Response {
	var args []string
	switch mode {
	case "yes", "safe":
		args = []string{"-y", "upgrade"}
	case "full":
		args = []string{"-y", "full-upgrade"}
	case "dist":
		args = []string{"-y", "dist-upgrade"}
	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unknown upgrade mode: %s", mode)}
	}

	rc, stdout, stderr := runAptGet(args...)
	changed := !strings.Contains(stdout, "0 upgraded")

	if rc != 0 {
		return Response{
			Failed: true,
			RC:     rc,
			Stdout: stdout,
			Stderr: stderr,
			Msg:    "failed to upgrade system",
		}
	}

	return Response{Changed: changed, RC: rc, Stdout: stdout, Stderr: stderr}
}

func autoremove(purge bool) Response {
	args := []string{"-y", "autoremove"}
	if purge {
		args = append(args, "--purge")
	}

	rc, stdout, stderr := runAptGet(args...)
	changed := !strings.Contains(stdout, "0 to remove")

	if rc != 0 {
		return Response{
			Failed: true,
			RC:     rc,
			Stdout: stdout,
			Stderr: stderr,
			Msg:    "failed to autoremove",
		}
	}

	return Response{Changed: changed, RC: rc, Stdout: stdout, Stderr: stderr}
}

func isInstalled(pkg string) bool {
	pkgName := pkg
	if idx := strings.Index(pkg, "="); idx != -1 {
		pkgName = pkg[:idx]
	}

	cmd := exec.Command(dpkgQueryCmd, "-W", "-f=${Status}", pkgName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "install ok installed")
}

func runAptGet(args ...string) (int, string, string) {
	cmd := exec.Command(aptGetCmd, args...)
	cmd.Env = append(os.Environ(), envVars...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	rc := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			rc = exitErr.ExitCode()
		} else {
			rc = 1
		}
	}

	return rc, stdout.String(), stderr.String()
}
