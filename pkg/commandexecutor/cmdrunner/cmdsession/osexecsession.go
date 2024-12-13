package cmdsession

import (
	"os/exec"
)

type OSExecutor struct {
	exec.Cmd
}

func (e *OSExecutor) env() {
}
