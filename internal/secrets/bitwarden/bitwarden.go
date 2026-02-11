package bitwarden

import (
	"encoding/json"
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

// Provider implements secrets.Provider for Bitwarden CLI.
//
// Reference format: "<item-name>/<field>"
// Supported fields: password, username, notes, or a custom field name.
type Provider struct {
	Runner CommandRunner
}

// NewProvider creates a Provider using the real Bitwarden CLI.
func NewProvider() *Provider {
	return &Provider{Runner: ExecRunner{}}
}

// bwLogin represents the login object in a Bitwarden item.
type bwLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// bwField represents a custom field in a Bitwarden item.
type bwField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// bwItem represents the relevant parts of a Bitwarden item JSON response.
type bwItem struct {
	Name   string    `json:"name"`
	Notes  string    `json:"notes"`
	Login  *bwLogin  `json:"login"`
	Fields []bwField `json:"fields"`
}

// Lookup fetches a secret from Bitwarden.
// The reference must be "bw:<item>/<field>".
func (p *Provider) Lookup(reference string) (string, error) {
	// reference arrives as "bw:<item>/<field>" (prefix "!" already stripped by Resolver)
	ref := strings.TrimPrefix(reference, "bw:")

	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("bitwarden: invalid reference %q, expected <item>/<field>", ref)
	}
	itemName := parts[0]
	field := parts[1]

	output, err := p.Runner.Run("bw", "get", "item", itemName)
	if err != nil {
		return "", fmt.Errorf("bitwarden: failed to get item %q: %w", itemName, err)
	}

	var item bwItem
	if err := json.Unmarshal(output, &item); err != nil {
		return "", fmt.Errorf("bitwarden: failed to parse item %q: %w", itemName, err)
	}

	return extractField(item, field)
}

func extractField(item bwItem, field string) (string, error) {
	switch field {
	case "password":
		if item.Login == nil {
			return "", fmt.Errorf("bitwarden: item %q has no login data", item.Name)
		}
		return item.Login.Password, nil
	case "username":
		if item.Login == nil {
			return "", fmt.Errorf("bitwarden: item %q has no login data", item.Name)
		}
		return item.Login.Username, nil
	case "notes":
		return item.Notes, nil
	default:
		for _, f := range item.Fields {
			if f.Name == field {
				return f.Value, nil
			}
		}
		return "", fmt.Errorf("bitwarden: field %q not found in item %q", field, item.Name)
	}
}
