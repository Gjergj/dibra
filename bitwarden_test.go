package main

import (
	"fmt"
	"log"
	"testing"
)

func TestBitwarden(t *testing.T) {
	sess, err := unlockBitwarden("Gocarropatoca1")
	if err != nil {
		t.Fatalf("failed to unlock bitwarden: %v", err)
	}

	ssh, err := getBitwardenItem(sess, "vps_ssd_nodes")
	if err != nil {
		log.Fatalf("failed to get bitwarden item: %v", err)
	}
	fmt.Println(ssh)
}

func TestGetSecrets(t *testing.T) {
	sess, err := unlockBitwarden("")
	if err != nil {
		t.Fatalf("failed to unlock bitwarden: %v", err)
	}
	secrets, err := getSecrets(sess, map[string]string{
		"SSH_PASSWORD": "vps_ssd_nodes/password",
		"SSH_USER":     "vps_ssd_nodes/username",
		"SSH_HOST":     "vps_ssd_nodes/IP v4",
	})
	if err != nil {
		log.Fatalf("failed to get secrets: %v", err)
	}
	fmt.Println(secrets)
}
