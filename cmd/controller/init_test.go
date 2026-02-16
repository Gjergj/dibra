package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")

	if err := runInit([]string{root}); err != nil {
		t.Fatalf("runInit failed: %v", err)
	}

	modulePath := filepath.Join(root, "cue.mod", "module.cue")
	if _, err := os.Stat(modulePath); err != nil {
		t.Fatalf("module.cue missing: %v", err)
	}

	deployPath := filepath.Join(root, "deploy.cue")
	if data, err := os.ReadFile(deployPath); err != nil {
		t.Fatalf("deploy.cue missing: %v", err)
	} else if !strings.Contains(string(data), "tasks") {
		t.Fatalf("deploy.cue content unexpected")
	}

	inventoryPath := filepath.Join(root, "inventory.cue")
	if data, err := os.ReadFile(inventoryPath); err != nil {
		t.Fatalf("inventory.cue missing: %v", err)
	} else if !strings.Contains(string(data), "hosts") {
		t.Fatalf("inventory.cue content unexpected")
	}
}

func TestRunInitForce(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "deploy.cue"), []byte("old"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := runInit([]string{"--force", root}); err != nil {
		t.Fatalf("runInit --force failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "deploy.cue"))
	if err != nil {
		t.Fatalf("read deploy.cue failed: %v", err)
	}
	if string(data) == "old" {
		t.Fatalf("deploy.cue was not overwritten")
	}
}
