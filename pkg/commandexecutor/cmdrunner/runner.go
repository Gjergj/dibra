package cmdrunner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner/cmdsession"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	// "golang.org/x/crypto/ssh/knownhosts"
	"github.com/skeema/knownhosts"
	// "gigawatt.io/knownhosts"
)

type RunnerSession interface {
	String() string
	Run() error
	Start() error
	Wait() error
	Output() ([]byte, error)
	CombinedOutput() ([]byte, error)
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.ReadCloser, error)
	StderrPipe() (io.ReadCloser, error)
	Close() error
}

type FSController interface {
	Upload(localPath string, remotePath string) error
	MkdirAll(remotePath string) error
	Stat(remotePath string) (os.FileInfo, error)
}

type Command struct {
	Command  string
	Args     []string
	Env      map[string]string
	WithSudo bool
	// Input    string
}

type Runner interface {
	NewRunnerSession(cmd Command) (RunnerSession, error)
	SudoPassword() string
	NewFSOPerations() (FSController, error)
}

// SSHConfig holds SSH connection configuration
type SSHConfig struct {
	Host          string
	Port          int
	User          string
	Password      string // Optional: used for password auth
	PrivateKey    string // Path to private key file
	KeyPassphrase string // Optional: passphrase for encrypted private key
	Timeout       time.Duration
	KnownHosts    string // Path to known_hosts file
	AllowInsecure bool
}

// SSHExecutor handles SSH connections and command execution
type SSHConnection struct {
	config *SSHConfig
	client *ssh.Client
}

// NewSSHExecutor creates a new SSH executor
func NewSSHConnection(config *SSHConfig) *SSHConnection {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Port == 0 {
		config.Port = 22
	}
	return &SSHConnection{config: config}
}

// Connect establishes SSH connection
func (e *SSHConnection) Connect() error {
	var authMethods []ssh.AuthMethod

	// If private key is provided, use key-based authentication
	if e.config.PrivateKey != "" {
		keyAuth, err := e.getPrivateKeyAuth()
		if err != nil {
			return fmt.Errorf("failed to configure private key auth: %w", err)
		}
		authMethods = append(authMethods, keyAuth)
	}

	// If password is provided, add password authentication as fallback
	if e.config.Password != "" {
		authMethods = append(authMethods, ssh.Password(e.config.Password))
	}

	if len(authMethods) == 0 {
		return fmt.Errorf("no authentication methods provided")
	}

	var hostKeyCallback ssh.HostKeyCallback
	if e.config.AllowInsecure {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	} else {
		// Load known hosts file
		var err error
		hostKeyCallback, err = e.getHostKeyCallback()
		if err != nil {
			return fmt.Errorf("failed to configure host key verification: %w", err)
		}
	}

	sshConfig := &ssh.ClientConfig{
		User:            e.config.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         e.config.Timeout,
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", e.config.Host, e.config.Port), sshConfig)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	e.client = client
	return nil
}

func (e *SSHConnection) SudoPassword() string {
	return e.config.Password
}

func (e *SSHConnection) getPrivateKeyAuth() (ssh.AuthMethod, error) {
	keyBytes, err := os.ReadFile(e.config.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	var signer ssh.Signer
	if e.config.KeyPassphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(e.config.KeyPassphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(keyBytes)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return ssh.PublicKeys(signer), nil
}

func (e *SSHConnection) getHostKeyCallback() (ssh.HostKeyCallback, error) {
	var knownHostsFile string
	if e.config.KnownHosts != "" {
		knownHostsFile = e.config.KnownHosts
	} else {
		knownHostsFile = filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
	}
	if _, err := os.Stat(knownHostsFile); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("known_hosts file not found: %s", knownHostsFile)
		}
		return nil, fmt.Errorf("failed to stat known_hosts file: %w", err)
	}

	knownHosts, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load known_hosts: %w", err)
	}

	return knownHosts.HostKeyCallback(), nil
}

func (e *SSHConnection) NewRunnerSession(cmd Command) (RunnerSession, error) {
	sess, err := e.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	return cmdsession.NewSShRunnerSession(sess, cmd.Command, cmd.Args, cmd.Env, cmd.WithSudo), nil
}

type SftpFSOperations struct {
	sftpClient *sftp.Client
}

func (e *SSHConnection) NewFSOPerations() (FSController, error) {
	cl, err := sftp.NewClient(e.client)
	if err != nil {
		return nil, fmt.Errorf("failed to create sftp client: %w", err)
	}
	return &SftpFSOperations{
		sftpClient: cl,
	}, nil
}

func (e *SftpFSOperations) Upload(localPath string, remotePath string) error {

	// check if local path exists
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		return fmt.Errorf("local path does not exist: %s", localPath)
	}

	local, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer local.Close()

	// defer e.sftpClient.Close()
	err = e.sftpClient.MkdirAll(filepath.Dir(remotePath))
	if err != nil {
		return err
	}
	remote, err := e.sftpClient.Create(remotePath)
	if err != nil {
		return err
	}
	defer remote.Close()

	_, err = io.Copy(remote, local)
	return err
}

func (e *SftpFSOperations) MkdirAll(remotePath string) error {
	return e.sftpClient.MkdirAll(remotePath)
}

func (e *SftpFSOperations) Stat(remotePath string) (os.FileInfo, error) {
	return e.sftpClient.Stat(remotePath)
}

// // ExecuteSudoCommand executes a command with sudo
// func (e *SSHExecutor) ExecuteSudoCommand(command string) error {
// 	if e.client == nil {
// 		return fmt.Errorf("not connected")
// 	}

// 	// Create new session
// 	session, err := e.client.NewSession()
// 	if err != nil {
// 		return fmt.Errorf("failed to create session: %w", err)
// 	}
// 	e.session = session
// 	defer e.session.Close()

// 	// Setup I/O
// 	var stdoutBuf, stderrBuf bytes.Buffer
// 	session.Stdout = &stdoutBuf
// 	session.Stderr = &stderrBuf

// 	// Get stdin pipe
// 	stdin, err := session.StdinPipe()
// 	if err != nil {
// 		return fmt.Errorf("failed to get stdin pipe: %w", err)
// 	}
// 	e.sudoWriter = stdin

// 	// Prepare sudo command
// 	sudoCmd := fmt.Sprintf("sudo -S -p '' %s", command)

// 	// Start command
// 	if err := session.Start(sudoCmd); err != nil {
// 		return fmt.Errorf("failed to start command: %w", err)
// 	}

// 	// Write sudo password
// 	if _, err := fmt.Fprintf(stdin, "%s\n", e.config.SudoPassword); err != nil {
// 		return fmt.Errorf("failed to write sudo password: %w", err)
// 	}

// 	// Wait for command completion
// 	if err := session.Wait(); err != nil {
// 		exitErr, ok := err.(*ssh.ExitError)
// 		if ok {
// 			return fmt.Errorf("command failed with exit code %d: %s",
// 				exitErr.ExitStatus(), stderrBuf.String())
// 		}
// 		return fmt.Errorf("failed to wait for command: %w", err)
// 	}

// 	return nil
// }

// ExecuteSudoCommand executes a command with sudo
// func (e *SSHConnection) Execute(command string, env []string, sudoInfo *SudoInfo, params ...string) (string, string, error) {
// 	if e.client == nil {
// 		return "", "", fmt.Errorf("not connected")
// 	}

// 	// Create new session
// 	session, err := e.client.NewSession()
// 	if err != nil {
// 		return "", "", fmt.Errorf("failed to create session: %w", err)
// 	}

// 	// Set session env vars
// 	var ev []string
// 	for _, value := range env {
// 		ev = strings.Split(value, "=")
// 		if err = session.Setenv(ev[0], strings.Join(ev[1:], "=")); err != nil {
// 			return "", "", fmt.Errorf("failed to set env var: %w", err)
// 		}
// 	}

// 	defer session.Close()

// 	// Setup I/O
// 	var stdoutBuf, stderrBuf bytes.Buffer
// 	session.Stdout = &stdoutBuf
// 	session.Stderr = &stderrBuf

// 	// Get stdin pipe
// 	stdin, err := session.StdinPipe()
// 	if err != nil {
// 		return "", "", fmt.Errorf("failed to get stdin pipe: %w", err)
// 	}

// 	// if sudoInfo != nil {
// 	// 	e.sudoWriter = stdin
// 	// 	command = fmt.Sprintf("sudo -S -p '' %s", command)
// 	// }

// 	args := append([]string{command}, params...)
// 	command = strings.Join(args, " ")

// 	// Start command
// 	if err := session.Start(command); err != nil {
// 		return "", "", fmt.Errorf("failed to start command: %w", err)
// 	}

// 	if sudoInfo != nil {
// 		// Write sudo password
// 		if _, err := fmt.Fprintf(stdin, "%s\n", sudoInfo.Password); err != nil {
// 			return "", "", fmt.Errorf("failed to write sudo password: %w", err)
// 		}
// 	}
// 	// stdin.Close()
// 	// if keyPress != "" {

// 	// 	// _, err := stdin.Write([]byte(keyPress))
// 	// 	// if err != nil {
// 	// 	// 	return "", "", fmt.Errorf("failed to write key press: %w", err)
// 	// 	// }
// 	// 	// Write key press
// 	// 	// if _, err := fmt.Fprintf(stdin, "%s", keyPress); err != nil {
// 	// 	// 	return "", "", fmt.Errorf("failed to write key press: %w", err)
// 	// 	// }
// 	// }

// 	// Wait for command completion
// 	if err := session.Wait(); err != nil {
// 		exitErr, ok := err.(*ssh.ExitError)
// 		if ok {
// 			return "", "", fmt.Errorf("command failed with exit code %d: %s",
// 				exitErr.ExitStatus(), stderrBuf.String())
// 		}
// 		return "", "", fmt.Errorf("failed to wait for command: %w", err)
// 	}

// 	return stdoutBuf.String(), stderrBuf.String(), nil
// }

// Close closes the SSH connection
func (e *SSHConnection) Close() error {
	if e.client != nil {
		return e.client.Close()
	}
	return nil
}

// // ExecuteLongRunningService executes a long-running systemctl command
// func (e *SSHExecutor) ExecuteLongRunningService(serviceName, action string) error {
// 	command := fmt.Sprintf("systemctl %s %s", action, serviceName)

// 	// Create new session
// 	session, err := e.client.NewSession()
// 	if err != nil {
// 		return fmt.Errorf("failed to create session: %w", err)
// 	}
// 	defer session.Close()

// 	// Setup terminal modes
// 	modes := ssh.TerminalModes{
// 		ssh.ECHO:          0,     // Disable echoing
// 		ssh.TTY_OP_ISPEED: 14400, // Input speed = 14.4kbaud
// 		ssh.TTY_OP_OSPEED: 14400, // Output speed = 14.4kbaud
// 	}

// 	// Request pseudo terminal
// 	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
// 		return fmt.Errorf("failed to request pty: %w", err)
// 	}

// 	// Setup I/O pipes
// 	stdin, err := session.StdinPipe()
// 	if err != nil {
// 		return fmt.Errorf("failed to get stdin pipe: %w", err)
// 	}

// 	var stdoutBuf, stderrBuf bytes.Buffer
// 	session.Stdout = &stdoutBuf
// 	session.Stderr = &stderrBuf

// 	// Start command
// 	if err := session.Start(fmt.Sprintf("sudo -S -p '' %s", command)); err != nil {
// 		return fmt.Errorf("failed to start command: %w", err)
// 	}

// 	// Write sudo password
// 	time.Sleep(100 * time.Millisecond) // Small delay to ensure prompt is ready
// 	if _, err := fmt.Fprintf(stdin, "%s\n", e.config.SudoPassword); err != nil {
// 		return fmt.Errorf("failed to write sudo password: %w", err)
// 	}

// 	// Wait for command completion
// 	if err := session.Wait(); err != nil {
// 		exitErr, ok := err.(*ssh.ExitError)
// 		if ok {
// 			return fmt.Errorf("command failed with exit code %d: %s",
// 				exitErr.ExitStatus(), stderrBuf.String())
// 		}
// 		return fmt.Errorf("failed to wait for command: %w", err)
// 	}

// 	return nil
// }
