package inventory

import (
	"fmt"
	"testing"

	"github.com/gjergjiramku/dibra/internal/secrets"
)

// mockProvider returns preset values or errors.
type mockProvider struct {
	values map[string]string
	err    error
}

func (m *mockProvider) Lookup(ref string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	v, ok := m.values[ref]
	if !ok {
		return "", fmt.Errorf("mock: not found: %s", ref)
	}
	return v, nil
}

func TestResolveSecrets_HostVars(t *testing.T) {
	inv := &Inventory{
		Groups: map[string]*Group{
			"all": {
				Name: "all",
				Vars: map[string]interface{}{},
				HostVars: map[string]map[string]interface{}{
					"web1": {
						"ansible_ssh_pass": "!bw:web1/password",
						"ansible_host":     "192.168.1.10",
					},
				},
			},
		},
		Hosts: map[string]*HostEntry{
			"web1": {Name: "web1"},
		},
	}

	r := secrets.NewResolver()
	r.Register("bw", &mockProvider{
		values: map[string]string{"bw:web1/password": "resolved-pass"},
	})

	if err := inv.ResolveSecrets(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := inv.Groups["all"].HostVars["web1"]["ansible_ssh_pass"]
	if got != "resolved-pass" {
		t.Errorf("ansible_ssh_pass = %v, want %q", got, "resolved-pass")
	}
	// Non-secret value unchanged
	host := inv.Groups["all"].HostVars["web1"]["ansible_host"]
	if host != "192.168.1.10" {
		t.Errorf("ansible_host = %v, want %q", host, "192.168.1.10")
	}
}

func TestResolveSecrets_GroupVars(t *testing.T) {
	inv := &Inventory{
		Groups: map[string]*Group{
			"all": {
				Name: "all",
				Vars: map[string]interface{}{
					"shared_password": "!op://vault/shared/password",
					"plain_var":       "unchanged",
				},
				HostVars: map[string]map[string]interface{}{},
			},
		},
		Hosts: map[string]*HostEntry{},
	}

	r := secrets.NewResolver()
	r.Register("op", &mockProvider{
		values: map[string]string{"op://vault/shared/password": "op-resolved"},
	})

	if err := inv.ResolveSecrets(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := inv.Groups["all"].Vars["shared_password"]
	if got != "op-resolved" {
		t.Errorf("shared_password = %v, want %q", got, "op-resolved")
	}
	plain := inv.Groups["all"].Vars["plain_var"]
	if plain != "unchanged" {
		t.Errorf("plain_var = %v, want %q", plain, "unchanged")
	}
}

func TestResolveSecrets_PlainUnchanged(t *testing.T) {
	inv := &Inventory{
		Groups: map[string]*Group{
			"webservers": {
				Name: "webservers",
				Vars: map[string]interface{}{
					"http_port": 80,
					"env":       "production",
					"enabled":   true,
				},
				HostVars: map[string]map[string]interface{}{
					"web1": {
						"ansible_host": "10.0.0.1",
						"ansible_port": 22,
					},
				},
			},
		},
		Hosts: map[string]*HostEntry{
			"web1": {Name: "web1"},
		},
	}

	r := secrets.NewResolver()
	// No providers registered — only plain values exist

	if err := inv.ResolveSecrets(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.Groups["webservers"].Vars["http_port"] != 80 {
		t.Error("int value changed")
	}
	if inv.Groups["webservers"].Vars["env"] != "production" {
		t.Error("string value changed")
	}
	if inv.Groups["webservers"].Vars["enabled"] != true {
		t.Error("bool value changed")
	}
}

func TestResolveSecrets_ErrorPropagation(t *testing.T) {
	inv := &Inventory{
		Groups: map[string]*Group{
			"all": {
				Name: "all",
				Vars: map[string]interface{}{
					"bad_secret": "!bw:locked/password",
				},
				HostVars: map[string]map[string]interface{}{},
			},
		},
		Hosts: map[string]*HostEntry{},
	}

	r := secrets.NewResolver()
	r.Register("bw", &mockProvider{
		err: fmt.Errorf("vault is locked"),
	})

	err := inv.ResolveSecrets(r)
	if err == nil {
		t.Fatal("expected error to propagate from provider")
	}
}

func TestResolveSecrets_NestedValues(t *testing.T) {
	inv := &Inventory{
		Groups: map[string]*Group{
			"all": {
				Name: "all",
				Vars: map[string]interface{}{
					"credentials": map[string]interface{}{
						"ssh_pass": "!bw:server/password",
						"api_key":  "!bw:server/api_key",
					},
				},
				HostVars: map[string]map[string]interface{}{},
			},
		},
		Hosts: map[string]*HostEntry{},
	}

	r := secrets.NewResolver()
	r.Register("bw", &mockProvider{
		values: map[string]string{
			"bw:server/password": "pass123",
			"bw:server/api_key":  "key456",
		},
	})

	if err := inv.ResolveSecrets(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds := inv.Groups["all"].Vars["credentials"].(map[string]interface{})
	if creds["ssh_pass"] != "pass123" {
		t.Errorf("ssh_pass = %v, want %q", creds["ssh_pass"], "pass123")
	}
	if creds["api_key"] != "key456" {
		t.Errorf("api_key = %v, want %q", creds["api_key"], "key456")
	}
}

func TestResolveSecrets_NilResolver(t *testing.T) {
	inv := &Inventory{
		Groups: map[string]*Group{
			"all": {
				Name: "all",
				Vars: map[string]interface{}{
					"password": "!bw:item/pass",
				},
				HostVars: map[string]map[string]interface{}{},
			},
		},
	}

	// Nil resolver should be a no-op
	if err := inv.ResolveSecrets(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Value should remain unchanged
	if inv.Groups["all"].Vars["password"] != "!bw:item/pass" {
		t.Error("nil resolver should not change values")
	}
}
