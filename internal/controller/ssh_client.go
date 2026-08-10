package controller

import "github.com/gjergjiramku/dibra/internal/ssh"

// sshExecutionClient keeps SSH reconnection behind the same boundary used for
// all other controller-side host operations.
type sshExecutionClient struct {
	*ssh.Client
	config ssh.Config
}

func newSSHExecutionClient(client *ssh.Client, config ssh.Config) *sshExecutionClient {
	return &sshExecutionClient{Client: client, config: config}
}

func (c *sshExecutionClient) IsLocal() bool { return false }

func (c *sshExecutionClient) Reconnect() (ExecutionClient, error) {
	client, err := ssh.Connect(c.config)
	if err != nil {
		return nil, err
	}
	return newSSHExecutionClient(client, c.config), nil
}
