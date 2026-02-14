package ssh

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type Client struct {
	conn           *ssh.Client
	host           string
	user           string
	become         bool
	becomePassword string
}

type Config struct {
	Host           string
	Port           int
	User           string
	Password       string
	SSHKeyPath     string
	Become         bool
	BecomePassword string
	Verbose        bool
}

func Connect(cfg Config) (*Client, error) {
	// Trim whitespace from host and user to prevent DNS lookup failures
	// (e.g., "  89.167.2.178" would be treated as a hostname instead of IP)
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.User = strings.TrimSpace(cfg.User)

	var authMethods []ssh.AuthMethod

	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
		if cfg.Verbose {
			fmt.Printf("  SSH: using password authentication\n")
		}
	}

	if cfg.SSHKeyPath != "" {
		if cfg.Verbose {
			fmt.Printf("  SSH: loading key from %s\n", cfg.SSHKeyPath)
		}
		key, err := os.ReadFile(cfg.SSHKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read SSH key: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to parse SSH key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
		if cfg.Verbose {
			fmt.Printf("  SSH: using public key authentication\n")
		}
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no authentication method provided")
	}

	sshConfig := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	if cfg.Verbose {
		fmt.Printf("  SSH: connecting to %s as %s\n", addr, cfg.User)
	}
	conn, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	if cfg.Verbose {
		fmt.Printf("  SSH: connected successfully (remote: %s)\n", conn.RemoteAddr())
	}

	return &Client{
		conn:           conn,
		host:           cfg.Host,
		user:           cfg.User,
		become:         cfg.Become,
		becomePassword: cfg.BecomePassword,
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) UploadFile(localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("failed to read local file: %w", err)
	}

	session, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	w, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	var stderr bytes.Buffer
	session.Stderr = &stderr

	dir := filepath.Dir(remotePath)
	if err := session.Start(fmt.Sprintf("scp -t %s", dir)); err != nil {
		return fmt.Errorf("failed to start scp: %w", err)
	}

	filename := filepath.Base(remotePath)
	fmt.Fprintf(w, "C0755 %d %s\n", len(data), filename)
	_, _ = w.Write(data)
	fmt.Fprint(w, "\x00")
	w.Close()

	if err := session.Wait(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return fmt.Errorf("scp failed: %w (stderr: %s)", err, errMsg)
		}
		return fmt.Errorf("scp failed: %w", err)
	}

	return nil
}

func (c *Client) FileExists(remotePath string) (bool, error) {
	session, err := c.conn.NewSession()
	if err != nil {
		return false, err
	}
	defer session.Close()

	err = session.Run(fmt.Sprintf("test -f %s", remotePath))
	return err == nil, nil
}

func (c *Client) ExecuteAgent(agentPath string, input []byte) ([]byte, error) {
	session, err := c.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var cmd string
	var stdinData []byte

	if c.become && c.user != "root" {
		cmd = fmt.Sprintf("sudo -S -p '' %s", agentPath)
		if c.becomePassword != "" {
			stdinData = append([]byte(c.becomePassword+"\n"), input...)
		} else {
			stdinData = input
		}
	} else {
		cmd = agentPath
		stdinData = input
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Start(cmd); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	go func() {
		_, _ = stdin.Write(stdinData)
		stdin.Close()
	}()

	if err := session.Wait(); err != nil {
		if _, ok := err.(*ssh.ExitError); !ok {
			return nil, fmt.Errorf("command failed: %w\nstderr: %s", err, stderr.String())
		}
	}

	return stdout.Bytes(), nil
}

func (c *Client) Run(cmd string) (string, string, error) {
	session, err := c.conn.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmd)
	return stdout.String(), stderr.String(), err
}

func (c *Client) RunWithSudo(cmd string) (string, string, error) {
	session, err := c.conn.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	stdin, _ := session.StdinPipe()

	fullCmd := fmt.Sprintf("sudo -S -p '' %s", cmd)
	if err := session.Start(fullCmd); err != nil {
		return "", "", err
	}

	go func() {
		if c.becomePassword != "" {
			_, _ = io.WriteString(stdin, c.becomePassword+"\n")
		}
		stdin.Close()
	}()

	err = session.Wait()
	return stdout.String(), stderr.String(), err
}

func (c *Client) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *Client) DownloadFile(remotePath, localPath string) error {
	session, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create local directory: %w", err)
	}

	cmd := fmt.Sprintf("/usr/bin/scp -f %s", remotePath)
	if c.become && c.user != "root" {
		cmd = fmt.Sprintf("sudo -S -p '' /usr/bin/scp -f %s", remotePath)
	}

	if err := session.Start(cmd); err != nil {
		return fmt.Errorf("failed to start scp: %w", err)
	}

	if c.become && c.user != "root" && c.becomePassword != "" {
		if _, err := stdin.Write([]byte(c.becomePassword + "\n")); err != nil {
			return fmt.Errorf("failed to write sudo password: %w", err)
		}
	}

	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("failed to send initial ack: %w", err)
	}

	buf := make([]byte, 1)
	if _, err := stdout.Read(buf); err != nil {
		return fmt.Errorf("failed to read protocol: %w", err)
	}
	if buf[0] != 'C' {
		return fmt.Errorf("unexpected scp protocol response: %c", buf[0])
	}

	header := make([]byte, 0, 1024)
	for {
		if _, err := stdout.Read(buf); err != nil {
			return fmt.Errorf("failed to read header: %w", err)
		}
		if buf[0] == '\n' {
			break
		}
		header = append(header, buf[0])
	}

	var mode string
	var size int64
	var filename string
	if _, err := fmt.Sscanf(string(header), "%s %d %s", &mode, &size, &filename); err != nil {
		return fmt.Errorf("failed to parse scp header: %w", err)
	}

	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("failed to send ack: %w", err)
	}

	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer localFile.Close()

	if _, err := io.CopyN(localFile, stdout, size); err != nil {
		return fmt.Errorf("failed to copy file data: %w", err)
	}

	if _, err := stdout.Read(buf); err != nil {
		return fmt.Errorf("failed to read final byte: %w", err)
	}

	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("failed to send final ack: %w", err)
	}
	stdin.Close()

	if err := session.Wait(); err != nil {
		if _, ok := err.(*ssh.ExitError); !ok {
			return fmt.Errorf("scp failed: %w", err)
		}
	}

	return nil
}
