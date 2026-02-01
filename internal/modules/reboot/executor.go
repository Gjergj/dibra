package reboot

import (
	"fmt"
	"os/exec"
	"strings"
)

func Execute(req Request) Response {
	req.SetDefaults()

	rebootCmd := req.RebootCommand
	if rebootCmd == "" {
		rebootCmd = findShutdownCommand(req.SearchPaths)
		if rebootCmd == "" {
			return Response{
				Failed: true,
				Msg:    "shutdown command not found in search paths",
			}
		}

		delayMin := req.PreRebootDelay / 60
		rebootCmd = fmt.Sprintf("%s -r +%d \"%s\"", rebootCmd, delayMin, req.Msg)
	}

	cmd := exec.Command("/bin/sh", "-c", rebootCmd)
	output, err := cmd.CombinedOutput()

	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok && exitErr.ExitCode() != 0 {
			return Response{
				Failed: true,
				Msg:    fmt.Sprintf("reboot command failed: %s, output: %s", err, string(output)),
			}
		}
	}

	return Response{
		Changed:  true,
		Rebooted: true,
		Msg:      "reboot initiated",
	}
}

func findShutdownCommand(searchPaths []string) string {
	for _, path := range searchPaths {
		fullPath := path + "/shutdown"
		if fileExists(fullPath) {
			return fullPath
		}
	}

	for _, path := range searchPaths {
		fullPath := path + "/reboot"
		if fileExists(fullPath) {
			return fullPath
		}
	}

	return ""
}

func fileExists(path string) bool {
	cmd := exec.Command("test", "-x", path)
	return cmd.Run() == nil
}

func GetBootTime(bootTimeCommand string) (string, error) {
	if bootTimeCommand == "" {
		bootTimeCommand = "cat /proc/sys/kernel/random/boot_id"
	}

	cmd := exec.Command("/bin/sh", "-c", bootTimeCommand)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get boot time: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func RunTestCommand(testCommand string) error {
	if testCommand == "" {
		testCommand = "whoami"
	}

	cmd := exec.Command("/bin/sh", "-c", testCommand)
	_, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("test command failed: %w", err)
	}

	return nil
}
