package onepassword

import (
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner abstracts command execution for testability.
type CommandRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands via os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// Provider implements secrets.Provider for 1Password CLI.
//
// Reference format: "op://vault/item/field" (1Password's native secret reference syntax).
type Provider struct {
	Runner CommandRunner
}

// NewProvider creates a Provider using the real 1Password CLI.
func NewProvider() *Provider {
	return &Provider{Runner: ExecRunner{}}
}

// Lookup fetches a secret from 1Password.
// The reference must be "op://vault/item/field".
func (p *Provider) Lookup(reference string) (string, error) {
	// reference arrives as "op://vault/item/field" (prefix "!" already stripped by Resolver)
	if !strings.HasPrefix(reference, "op://") {
		return "", fmt.Errorf("1password: invalid reference %q, expected op://vault/item/field", reference)
	}

	output, err := p.Runner.Run("op", "read", reference)
	if err != nil {
		return "", fmt.Errorf("1password: failed to read %q: %w", reference, err)
	}

	return strings.TrimSpace(string(output)), nil
}
