package main

type CommandExecutor interface {
	Execute(command string, sudoInfo *SudoInfo, args ...string) (string, error)
}

type SudoInfo struct {
	Password string
}
