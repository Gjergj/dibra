package onepassword

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

func TestLookup_ValidReference(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		output: []byte("my-secret-value"),
	}}
	got, err := p.Lookup("op://vault/item/password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-secret-value" {
		t.Errorf("got %q, want %q", got, "my-secret-value")
	}
}

func TestLookup_InvalidReference(t *testing.T) {
	p := &Provider{Runner: &mockRunner{}}
	_, err := p.Lookup("not-a-valid-reference")
	if err == nil {
		t.Fatal("expected error for invalid reference")
	}
}

func TestLookup_NotFound(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		err: fmt.Errorf("exit status 1"),
	}}
	_, err := p.Lookup("op://vault/nonexistent/password")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

func TestLookup_WhitespaceTrimmed(t *testing.T) {
	p := &Provider{Runner: &mockRunner{
		output: []byte("  my-secret-value  \n"),
	}}
	got, err := p.Lookup("op://vault/item/password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-secret-value" {
		t.Errorf("got %q, want %q (whitespace not trimmed)", got, "my-secret-value")
	}
}
