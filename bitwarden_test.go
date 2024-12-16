package main

import (
	"fmt"
	"testing"
)

func TestBitwarden(t *testing.T) {
	sess, err := unlockBitwarden()
	if err != nil {
		t.Fatalf("failed to unlock bitwarden: %v", err)
	}
	fmt.Println(sess)
}
