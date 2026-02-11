package bitwarden

import (
	"fmt"
	"testing"
)

type mockRunner struct {
	output []byte
	err    error
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	return m.output, m.err
}

func TestLookup_PasswordField(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		output: []byte(`{"name":"myserver","login":{"username":"admin","password":"s3cret"},"notes":"","fields":[]}`),
	}}
	got, err := p.Lookup("bw:myserver/password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("got %q, want %q", got, "s3cret")
	}
}

func TestLookup_UsernameField(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		output: []byte(`{"name":"myserver","login":{"username":"admin","password":"s3cret"}}`),
	}}
	got, err := p.Lookup("bw:myserver/username")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "admin" {
		t.Errorf("got %q, want %q", got, "admin")
	}
}

func TestLookup_NotesField(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		output: []byte(`{"name":"myserver","login":{"username":"admin","password":"s3cret"},"notes":"ssh key content here"}`),
	}}
	got, err := p.Lookup("bw:myserver/notes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ssh key content here" {
		t.Errorf("got %q, want %q", got, "ssh key content here")
	}
}

func TestLookup_CustomField(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		output: []byte(`{"name":"myserver","login":{"username":"admin","password":"s3cret"},"fields":[{"name":"ssh_port","value":"2222"},{"name":"api_key","value":"abc123"}]}`),
	}}
	got, err := p.Lookup("bw:myserver/api_key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "abc123" {
		t.Errorf("got %q, want %q", got, "abc123")
	}
}

func TestLookup_InvalidReference_NoSlash(t *testing.T) {
	p := &Provider{Runner: &mockRunner{}}
	_, err := p.Lookup("bw:myserver")
	if err == nil {
		t.Fatal("expected error for missing field separator")
	}
}

func TestLookup_InvalidReference_EmptyItem(t *testing.T) {
	p := &Provider{Runner: &mockRunner{}}
	_, err := p.Lookup("bw:/password")
	if err == nil {
		t.Fatal("expected error for empty item name")
	}
}

func TestLookup_InvalidReference_EmptyField(t *testing.T) {
	p := &Provider{Runner: &mockRunner{}}
	_, err := p.Lookup("bw:myserver/")
	if err == nil {
		t.Fatal("expected error for empty field name")
	}
}

func TestLookup_ItemNotFound(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		err: fmt.Errorf("exit status 1"),
	}}
	_, err := p.Lookup("bw:nonexistent/password")
	if err == nil {
		t.Fatal("expected error for item not found")
	}
}

func TestLookup_FieldNotFound(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		output: []byte(`{"name":"myserver","login":{"username":"admin","password":"s3cret"},"fields":[]}`),
	}}
	_, err := p.Lookup("bw:myserver/nonexistent_field")
	if err == nil {
		t.Fatal("expected error for field not found")
	}
}

func TestLookup_InvalidJSON(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		output: []byte(`not valid json`),
	}}
	_, err := p.Lookup("bw:myserver/password")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLookup_NoLoginData(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		output: []byte(`{"name":"myserver","notes":"just notes","fields":[]}`),
	}}
	_, err := p.Lookup("bw:myserver/password")
	if err == nil {
		t.Fatal("expected error for missing login data")
	}
}
