package docker

import (
	"testing"
	"time"
)

func TestGetContextWithEnvironmentUsesResolvedTimeout(t *testing.T) {
	started := time.Now()
	ctx, cancel := GetContextWithEnvironment(CommonArgs{}, StaticEnvironment{"DOCKER_TIMEOUT": "7"})
	defer cancel()

	deadline, found := ctx.Deadline()
	if !found {
		t.Fatal("context has no deadline")
	}
	if remaining := deadline.Sub(started); remaining < 6*time.Second || remaining > 8*time.Second {
		t.Fatalf("deadline remaining = %s, want about 7s", remaining)
	}
}

func TestGetContextExplicitZeroSuppressesEnvironmentTimeout(t *testing.T) {
	ctx, cancel := GetContextWithEnvironment(CommonArgs{Timeout: pointer(0)}, StaticEnvironment{"DOCKER_TIMEOUT": "7"})
	defer cancel()

	if _, found := ctx.Deadline(); found {
		t.Fatal("explicit timeout 0 should disable the deadline")
	}
}
