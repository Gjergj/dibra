package docker

import (
	"strings"
	"testing"
)

func TestImageReferenceNormalization(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "nginx", want: "docker.io/library/nginx"},
		{input: "nginx:1.25.3", want: "docker.io/library/nginx:1.25.3"},
		{input: "index.docker.io/example/app:tag", want: "docker.io/example/app:tag"},
		{input: "registry.hub.docker.com/nginx@sha256:d02f9b9db4d759ef27dc26b426b842ff2fb881c5c6079612d27ec36e36b132dd", want: "docker.io/library/nginx@sha256:d02f9b9db4d759ef27dc26b426b842ff2fb881c5c6079612d27ec36e36b132dd"},
		{input: "localhost:5000/example/app:latest", want: "localhost:5000/example/app:latest"},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := NormalizeImageReference(test.input)
			if err != nil {
				t.Fatalf("NormalizeImageReference() error = %v", err)
			}
			if got != test.want {
				t.Errorf("NormalizeImageReference() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestImageReferenceValidation(t *testing.T) {
	for _, value := range []string{"", "-bad/repo", "repo:-bad", "repo@sha256:abc", "Upper/Case"} {
		t.Run(value, func(t *testing.T) {
			if _, err := NormalizeImageReference(value); err == nil {
				t.Fatalf("NormalizeImageReference(%q) unexpectedly succeeded", value)
			}
		})
	}
}

func TestJoinImageNameTagPreservesInlineTagAndDigest(t *testing.T) {
	digest := "sha256:d02f9b9db4d759ef27dc26b426b842ff2fb881c5c6079612d27ec36e36b132dd"
	tests := []struct {
		name string
		tag  string
		want string
	}{
		{name: "example/app", tag: "v1", want: "example/app:v1"},
		{name: "example/app:v2", tag: "v1", want: "example/app:v2"},
		{name: "example/app@" + digest, tag: "v1", want: "example/app@" + digest},
	}
	for _, test := range tests {
		got, err := JoinImageNameTag(test.name, test.tag)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Errorf("JoinImageNameTag(%q, %q) = %q, want %q", test.name, test.tag, got, test.want)
		}
	}
}

func TestImageReferenceHostnamePort(t *testing.T) {
	host, port, err := ParseImageReference("docker.io/library/nginx").HostnamePort()
	if err != nil || host != "index.docker.io" || port != 443 {
		t.Fatalf("HostnamePort() = %q, %d, %v", host, port, err)
	}
	host, port, err = ParseImageReference("registry.example.test:5443/app").HostnamePort()
	if err != nil || host != "registry.example.test" || port != 5443 {
		t.Fatalf("HostnamePort() = %q, %d, %v", host, port, err)
	}
}

func TestImageIDAndTagValidation(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	if !IsImageID(id) || !IsImageID("sha256:"+strings.Repeat("A", 64)) {
		t.Fatal("canonical full image IDs were rejected")
	}
	for _, value := range []string{strings.Repeat("a", 64), "sha256:" + strings.Repeat("a", 12), "sha256:not-hex"} {
		if IsImageID(value) {
			t.Fatalf("IsImageID(%q) unexpectedly succeeded", value)
		}
	}
	if !IsValidImageTag("release_2026.08-1", false) || IsValidImageTag("foo/bar", false) ||
		IsValidImageTag("", false) || !IsValidImageTag("", true) {
		t.Fatal("image tag validation did not match the pinned contract")
	}
}
