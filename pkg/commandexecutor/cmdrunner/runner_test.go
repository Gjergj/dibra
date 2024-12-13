package cmdrunner_test

import (
	"log"
	"testing"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner"
)

func TestRunner(t *testing.T) {
	config := &cmdrunner.SSHConfig{
		Host:     "89.233.108.20",
		Port:     22,
		User:     "",
		Password: "",
		Timeout:  30 * time.Second,
	}

	sshConnection := cmdrunner.NewSSHConnection(config)
	defer sshConnection.Close()

	// Connect to remote server
	if err := sshConnection.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
}

func TestRunnerWithPrivateKey(t *testing.T) {
	// Create SSH connection with key-based authentication
	config := &cmdrunner.SSHConfig{
		Host:       "localhost",
		Port:       32222,
		User:       "default",
		PrivateKey: "/Users/gjergjiramku/.orbstack/ssh/id_ed25519",
		// KeyPassphrase: "optional-passphrase", // Leave empty if key is not encrypted
		Timeout: 30 * time.Second,
		// KnownHosts:    "/Users/gjergjiramku/.orbstack/ssh/authorized_keys",
		AllowInsecure: true,
	}

	sshConn := cmdrunner.NewSSHConnection(config)
	err := sshConn.Connect()
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer sshConn.Close()
}
