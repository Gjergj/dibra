package apt

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	aptGetCmd    = "/usr/bin/apt-get"
	dpkgQueryCmd = "/usr/bin/dpkg-query"
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
	if cacheValidTime > 0 {
		listDir := "/var/lib/apt/lists"
		info, err := os.Stat(listDir)
		if err == nil {
			age := time.Since(info.ModTime())
			if age.Seconds() < float64(cacheValidTime) {
				return Response{Changed: false, Msg: "cache is still valid"}
			}
		}
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

	return Response{Changed: true, RC: rc, Stdout: stdout, Stderr: stderr}
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
