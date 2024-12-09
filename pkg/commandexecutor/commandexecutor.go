package commandexecutor

type CommandExecutor interface {
	Execute(command string, env []string, sudoInfo *SudoInfo, args ...string) (string, string, error)
}

type Command interface {
}

type SudoInfo struct {
	Password string
}

const EOF KeyPress = "\x04" // EOF character CTRL-D

type KeyPress string
