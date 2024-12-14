package cmdsession

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"
)

type SShRunnerSession struct {
	command  string
	args     []string
	env      map[string]string
	withSudo bool
	session  *ssh.Session
}

func NewSShRunnerSession(session *ssh.Session, command string, args []string, env map[string]string, withSudo bool) *SShRunnerSession {
	return &SShRunnerSession{session: session, command: command, args: args, env: env, withSudo: withSudo}
}

func (c *SShRunnerSession) String() string {
	b := new(strings.Builder)
	if c.withSudo {
		b.WriteString("sudo -S ")
	}
	b.WriteString(c.command)
	for _, a := range c.args {
		b.WriteByte(' ')
		b.WriteString(a)
	}
	return b.String()
}

func (e *SShRunnerSession) Run() error {
	for k, v := range e.env {
		if err := e.session.Setenv(k, v); err != nil {
			return fmt.Errorf("failed to set env var: %w", err)
		}
	}
	return e.session.Run(e.String())
}

func (e *SShRunnerSession) Start() error {
	// Set session env vars
	for k, v := range e.env {
		if err := e.session.Setenv(k, v); err != nil {
			return fmt.Errorf("failed to set env var: %w", err)
		}
	}
	// args := append([]string{e.Command}, e.Args...)
	// cmd := strings.Join(args, " ")
	cmd := e.String()

	// Start command
	return e.session.Start(cmd)
}

// func (e *SSHCommand) Error() string {
// 	return e.session.Err().Error()
// 	return ""
// }

func (e *SShRunnerSession) Wait() error {
	return e.session.Wait()
}

func (e *SShRunnerSession) Output() ([]byte, error) {
	return e.session.Output(e.String())
}

func (e *SShRunnerSession) CombinedOutput() ([]byte, error) {
	for k, v := range e.env {
		if err := e.session.Setenv(k, v); err != nil {
			return nil, fmt.Errorf("failed to set env var: %w", err)
		}
	}
	return e.session.CombinedOutput(e.String())
}

func (e *SShRunnerSession) StdinPipe() (io.WriteCloser, error) {
	return e.session.StdinPipe()
}

func (e *SShRunnerSession) StdoutPipe() (io.ReadCloser, error) {
	p, err := e.session.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	return io.NopCloser(p), nil
}
func (e *SShRunnerSession) StderrPipe() (io.ReadCloser, error) {
	p, err := e.session.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}
	return io.NopCloser(p), nil
}

func (e *SShRunnerSession) Close() error {
	return e.session.Close()
}

// func (e *SSHCommand) Environ() ([]string, error) {
// 	return e.environ()
// }
