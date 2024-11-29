package commandexecutor

type CommandExecutor interface {
	Execute(command string, sudoInfo *SudoInfo, args ...string) (string, string, error)
}

type SudoInfo struct {
	Password string
}
