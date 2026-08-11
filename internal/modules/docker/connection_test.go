package docker

import (
	"reflect"
	"strings"
	"testing"
)

var dockerEnvironmentKeys = []string{
	"DOCKER_HOST",
	"DOCKER_API_VERSION",
	"DOCKER_TIMEOUT",
	"DOCKER_TLS",
	"DOCKER_TLS_VERIFY",
	"DOCKER_CERT_PATH",
	"DOCKER_TLS_HOSTNAME",
	"DOCKER_CONTEXT",
}

func clearDockerEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range dockerEnvironmentKeys {
		t.Setenv(key, "")
	}
}

func TestResolveConnectionDefaults(t *testing.T) {
	clearDockerEnvironment(t)

	got, err := ResolveConnection(CommonArgs{})
	if err != nil {
		t.Fatalf("ResolveConnection() error = %v", err)
	}
	if got.DockerHost != DefaultDockerHost {
		t.Errorf("DockerHost = %q, want %q", got.DockerHost, DefaultDockerHost)
	}
	if got.APIVersion != "auto" {
		t.Errorf("APIVersion = %q, want auto", got.APIVersion)
	}
	if got.Timeout != DefaultTimeoutSeconds {
		t.Errorf("Timeout = %d, want %d", got.Timeout, DefaultTimeoutSeconds)
	}
	if got.TLS || got.ValidateCerts {
		t.Errorf("TLS defaults = (%t, %t), want false", got.TLS, got.ValidateCerts)
	}
}

func TestResolveConnectionEnvironmentFallbacks(t *testing.T) {
	clearDockerEnvironment(t)
	t.Setenv("DOCKER_HOST", "tcp://daemon.example:2376")
	t.Setenv("DOCKER_API_VERSION", "1.44")
	t.Setenv("DOCKER_TIMEOUT", "17")
	t.Setenv("DOCKER_TLS", "yes")
	t.Setenv("DOCKER_TLS_VERIFY", "on")
	t.Setenv("DOCKER_CERT_PATH", "/docker-certs")
	t.Setenv("DOCKER_TLS_HOSTNAME", "tls.example")

	got, err := ResolveConnection(CommonArgs{})
	if err != nil {
		t.Fatalf("ResolveConnection() error = %v", err)
	}
	if got.DockerHost != "tcp://daemon.example:2376" || got.APIVersion != "1.44" || got.Timeout != 17 {
		t.Fatalf("unexpected basic options: %#v", got)
	}
	if !got.TLS || !got.ValidateCerts {
		t.Fatalf("TLS options = (%t, %t), want true", got.TLS, got.ValidateCerts)
	}
	if got.CAPath != "/docker-certs/ca.pem" || got.ClientCert != "/docker-certs/cert.pem" || got.ClientKey != "/docker-certs/key.pem" {
		t.Errorf("certificate paths were not resolved from DOCKER_CERT_PATH: %#v", got)
	}
	if got.TLSHostname != "tls.example" {
		t.Errorf("TLSHostname = %q, want tls.example", got.TLSHostname)
	}
}

func TestResolveConnectionArgumentsOverrideEnvironment(t *testing.T) {
	clearDockerEnvironment(t)
	t.Setenv("DOCKER_HOST", "tcp://environment:2375")
	t.Setenv("DOCKER_API_VERSION", "1.40")
	t.Setenv("DOCKER_TIMEOUT", "11")
	t.Setenv("DOCKER_CERT_PATH", "/environment-certs")
	t.Setenv("DOCKER_TLS_HOSTNAME", "environment.example")

	got, err := ResolveConnection(CommonArgs{
		DockerHost:  "tcp://argument:2376",
		APIVersion:  "1.55",
		Timeout:     23,
		CAPath:      "/args/ca.pem",
		ClientCert:  "/args/cert.pem",
		ClientKey:   "/args/key.pem",
		TLSHostname: "argument.example",
	})
	if err != nil {
		t.Fatalf("ResolveConnection() error = %v", err)
	}
	if got.DockerHost != "tcp://argument:2376" || got.APIVersion != "1.55" || got.Timeout != 23 {
		t.Fatalf("arguments did not override environment: %#v", got)
	}
	if got.CAPath != "/args/ca.pem" || got.ClientCert != "/args/cert.pem" || got.ClientKey != "/args/key.pem" {
		t.Errorf("certificate arguments did not override environment: %#v", got)
	}
	if got.TLSHostname != "argument.example" {
		t.Errorf("TLSHostname = %q, want argument.example", got.TLSHostname)
	}
}

func TestResolveConnectionValidatesSharedOptions(t *testing.T) {
	t.Run("certificate pair", func(t *testing.T) {
		clearDockerEnvironment(t)
		_, err := ResolveConnection(CommonArgs{ClientCert: "/cert.pem"})
		if err == nil || !strings.Contains(err.Error(), "specified together") {
			t.Fatalf("ResolveConnection() error = %v, want client certificate pair error", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		clearDockerEnvironment(t)
		t.Setenv("DOCKER_TIMEOUT", "never")
		_, err := ResolveConnection(CommonArgs{})
		if err == nil || !strings.Contains(err.Error(), "positive integer") {
			t.Fatalf("ResolveConnection() error = %v, want timeout error", err)
		}
	})

	t.Run("host and context", func(t *testing.T) {
		clearDockerEnvironment(t)
		_, err := ResolveConnection(CommonArgs{DockerHost: "unix:///tmp/docker.sock", CLIContext: "production"})
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("ResolveConnection() error = %v, want mutual exclusion error", err)
		}
	})
}

func TestDockerCLIConnection(t *testing.T) {
	clearDockerEnvironment(t)
	common := CommonArgs{
		DockerHost:    "tcp://daemon.example:2376",
		TLS:           true,
		ValidateCerts: true,
		CAPath:        "/certs/ca.pem",
		ClientCert:    "/certs/cert.pem",
		ClientKey:     "/certs/key.pem",
		TLSHostname:   "tls.example",
		APIVersion:    "1.55",
		Debug:         true,
	}

	gotArgs, err := DockerCLIArgs(common, "compose", "version")
	if err != nil {
		t.Fatalf("DockerCLIArgs() error = %v", err)
	}
	wantArgs := []string{
		"--host", "tcp://daemon.example:2376",
		"--tlsverify",
		"--tlscacert", "/certs/ca.pem",
		"--tlscert", "/certs/cert.pem",
		"--tlskey", "/certs/key.pem",
		"--debug",
		"compose", "version",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("DockerCLIArgs() = %#v, want %#v", gotArgs, wantArgs)
	}

	gotEnv, err := DockerCLIEnv(common)
	if err != nil {
		t.Fatalf("DockerCLIEnv() error = %v", err)
	}
	if !environmentContains(gotEnv, "DOCKER_API_VERSION=1.55") || !environmentContains(gotEnv, "DOCKER_TLS_HOSTNAME=tls.example") {
		t.Errorf("DockerCLIEnv() missing normalized values: %#v", gotEnv)
	}
}

func TestDockerCLIContextDoesNotInjectDefaultHost(t *testing.T) {
	clearDockerEnvironment(t)

	got, err := DockerCLIArgs(CommonArgs{CLIContext: "production"}, "info")
	if err != nil {
		t.Fatalf("DockerCLIArgs() error = %v", err)
	}
	want := []string{"--context", "production", "info"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DockerCLIArgs() = %#v, want %#v", got, want)
	}
}

func TestGetClientUsesResolvedHostAndAPIVersion(t *testing.T) {
	clearDockerEnvironment(t)

	cli, err := GetClient(CommonArgs{
		DockerHost: "unix:///tmp/dibra-docker.sock",
		APIVersion: "1.44",
		Timeout:    2,
	})
	if err != nil {
		t.Fatalf("GetClient() error = %v", err)
	}
	defer cli.Close()
	if cli.DaemonHost() != "unix:///tmp/dibra-docker.sock" {
		t.Errorf("DaemonHost() = %q", cli.DaemonHost())
	}
	if cli.ClientVersion() != "1.44" {
		t.Errorf("ClientVersion() = %q, want 1.44", cli.ClientVersion())
	}
}

func TestGetClientBuildsOpenSSHTransport(t *testing.T) {
	clearDockerEnvironment(t)

	cli, err := GetClient(CommonArgs{DockerHost: "ssh://deploy@docker.example", UseSSHClient: true})
	if err != nil {
		t.Fatalf("GetClient() error = %v", err)
	}
	defer cli.Close()
	if cli.DaemonHost() != "http://docker.example.com" {
		t.Errorf("DaemonHost() = %q, want connhelper host", cli.DaemonHost())
	}
}

func TestNewTLSConfigUsesResolvedHostname(t *testing.T) {
	config, err := newTLSConfig(ConnectionOptions{TLS: true, TLSHostname: "daemon.example"})
	if err != nil {
		t.Fatalf("newTLSConfig() error = %v", err)
	}
	if config.ServerName != "daemon.example" {
		t.Errorf("ServerName = %q, want daemon.example", config.ServerName)
	}
	if !config.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false, want true when validate_certs is disabled")
	}
}

func environmentContains(environment []string, want string) bool {
	for _, entry := range environment {
		if entry == want {
			return true
		}
	}
	return false
}
