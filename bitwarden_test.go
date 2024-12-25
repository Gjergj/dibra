package main

import (
	"fmt"
	"log"
	"testing"
)

func TestBitwarden(t *testing.T) {
	sess, err := unlockBitwarden("")
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
		"SSH_PORT":           "social_posts/port",
		"SSH_HOST":           "social_posts/host",
		"SSH_PASSWORD":       "vps_ssd_nodes/password",
		"SSH_USER":           "vps_ssd_nodes/username",
		"SSD_NODES_PASSWORD": "vps_ssd_nodes/password",
	})
	if err != nil {
		log.Fatalf("failed to get secrets: %v", err)
	}
	fmt.Println(secrets)
}
