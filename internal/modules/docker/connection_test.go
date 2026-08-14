package docker

import (
	"os"
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
		value, found := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		t.Cleanup(func() {
			if found {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		})
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
	t.Setenv("DOCKER_TLS", "true")
	t.Setenv("DOCKER_TLS_VERIFY", "true")

	got, err := ResolveConnection(CommonArgs{
		DockerHost:    pointer("tcp://argument:2376"),
		APIVersion:    pointer("1.55"),
		Timeout:       pointer(23),
		CAPath:        pointer("/args/ca.pem"),
		ClientCert:    pointer("/args/cert.pem"),
		ClientKey:     pointer("/args/key.pem"),
		TLSHostname:   pointer("argument.example"),
		TLS:           pointer(false),
		ValidateCerts: pointer(false),
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
	if got.TLS || got.ValidateCerts {
		t.Errorf("explicit false TLS arguments did not override environment: %#v", got)
	}
}

func TestResolveConnectionExplicitZeroValuesOverrideEnvironment(t *testing.T) {
	environment := StaticEnvironment{
		"DOCKER_HOST":         "tcp://environment:2376",
		"DOCKER_API_VERSION":  "1.55",
		"DOCKER_TIMEOUT":      "45",
		"DOCKER_CERT_PATH":    "/environment-certs",
		"DOCKER_TLS_HOSTNAME": "environment.example",
		"DOCKER_TLS":          "true",
		"DOCKER_TLS_VERIFY":   "true",
	}

	got, err := ResolveConnectionWithEnvironment(CommonArgs{
		DockerHost:    pointer(""),
		APIVersion:    pointer(""),
		Timeout:       pointer(0),
		CAPath:        pointer(""),
		ClientCert:    pointer(""),
		ClientKey:     pointer(""),
		TLSHostname:   pointer(""),
		TLS:           pointer(false),
		ValidateCerts: pointer(false),
	}, environment)
	if err != nil {
		t.Fatalf("ResolveConnectionWithEnvironment() error = %v", err)
	}
	if got.DockerHost != "" || got.APIVersion != "" || got.Timeout != 0 {
		t.Fatalf("explicit zero values did not override environment: %#v", got)
	}
	if got.CAPath != "" || got.ClientCert != "" || got.ClientKey != "" || got.TLSHostname != "" {
		t.Errorf("explicit empty TLS paths did not override environment: %#v", got)
	}
	if got.TLS || got.ValidateCerts {
		t.Errorf("explicit false TLS values did not override environment: %#v", got)
	}
}

func TestResolveConnectionValidatesSharedOptions(t *testing.T) {
	t.Run("certificate pair", func(t *testing.T) {
		clearDockerEnvironment(t)
		_, err := ResolveConnection(CommonArgs{ClientCert: pointer("/cert.pem")})
		if err == nil || !strings.Contains(err.Error(), "specified together") {
			t.Fatalf("ResolveConnection() error = %v, want client certificate pair error", err)
		}
	})

	t.Run("certificate environment does not complete explicit pair", func(t *testing.T) {
		_, err := ResolveConnectionWithEnvironment(
			CommonArgs{ClientCert: pointer("/cert.pem")},
			StaticEnvironment{"DOCKER_CERT_PATH": "/environment-certs"},
		)
		if err == nil || !strings.Contains(err.Error(), "specified together") {
			t.Fatalf("ResolveConnectionWithEnvironment() error = %v, want client certificate pair error", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		clearDockerEnvironment(t)
		t.Setenv("DOCKER_TIMEOUT", "never")
		_, err := ResolveConnection(CommonArgs{})
		if err == nil || !strings.Contains(err.Error(), "must be an integer") {
			t.Fatalf("ResolveConnection() error = %v, want timeout error", err)
		}
	})

	t.Run("host and context", func(t *testing.T) {
		clearDockerEnvironment(t)
		_, err := ResolveConnection(CommonArgs{DockerHost: pointer("unix:///tmp/docker.sock"), CLIContext: pointer("production")})
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("ResolveConnection() error = %v, want mutual exclusion error", err)
		}
	})
}

func TestDockerCLIConnection(t *testing.T) {
	clearDockerEnvironment(t)
	common := CommonArgs{
		DockerHost:    pointer("tcp://daemon.example:2376"),
		TLS:           pointer(true),
		ValidateCerts: pointer(true),
		CAPath:        pointer("/certs/ca.pem"),
		ClientCert:    pointer("/certs/cert.pem"),
		ClientKey:     pointer("/certs/key.pem"),
		TLSHostname:   pointer("tls.example"),
		APIVersion:    pointer("1.55"),
		Debug:         pointer(true),
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

	got, err := DockerCLIArgs(CommonArgs{CLIContext: pointer("production")}, "info")
	if err != nil {
		t.Fatalf("DockerCLIArgs() error = %v", err)
	}
	want := []string{"--context", "production", "info"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DockerCLIArgs() = %#v, want %#v", got, want)
	}
}

func TestDockerCLIContextConflictsWithSuppliedEmptyHost(t *testing.T) {
	_, err := DockerCLIArgsWithEnvironment(
		CommonArgs{CLIContext: pointer("production")},
		StaticEnvironment{"DOCKER_HOST": ""},
		"info",
	)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("DockerCLIArgsWithEnvironment() error = %v, want mutual exclusion error", err)
	}
}

func TestCLIAndAPIHostnameFallbacksRemainDistinct(t *testing.T) {
	environment := StaticEnvironment{"DOCKER_HOST": "tcp://daemon.example:2376"}

	apiConnection, err := ResolveConnectionWithEnvironment(CommonArgs{}, environment)
	if err != nil {
		t.Fatalf("ResolveConnectionWithEnvironment() error = %v", err)
	}
	if apiConnection.TLSHostname != "daemon.example" {
		t.Errorf("API TLSHostname = %q, want daemon.example", apiConnection.TLSHostname)
	}

	cliConnection, err := resolveCLIConnection(CommonArgs{}, environment)
	if err != nil {
		t.Fatalf("resolveCLIConnection() error = %v", err)
	}
	if cliConnection.TLSHostname != "" {
		t.Errorf("CLI TLSHostname = %q, want empty unless explicitly configured", cliConnection.TLSHostname)
	}
}

func TestDockerCLIEnvironmentUsesResolvedPrecedence(t *testing.T) {
	environment := StaticEnvironment{
		"DOCKER_HOST":         "tcp://environment:2376",
		"DOCKER_CONTEXT":      "ambient-context",
		"DOCKER_API_VERSION":  "1.44",
		"DOCKER_TLS_HOSTNAME": "environment.example",
		"UNCHANGED":           "value",
	}
	got, err := DockerCLIEnvWithEnvironment(CommonArgs{
		APIVersion:  pointer("1.55"),
		TLSHostname: pointer("argument.example"),
	}, environment)
	if err != nil {
		t.Fatalf("DockerCLIEnvWithEnvironment() error = %v", err)
	}
	for _, want := range []string{
		"DOCKER_API_VERSION=1.55",
		"DOCKER_TLS_HOSTNAME=argument.example",
		"UNCHANGED=value",
	} {
		if !environmentContains(got, want) {
			t.Errorf("environment missing %q: %#v", want, got)
		}
	}
	for _, removed := range []string{"DOCKER_HOST=", "DOCKER_CONTEXT="} {
		for _, entry := range got {
			if strings.HasPrefix(entry, removed) {
				t.Errorf("environment retained conflicting value %q", entry)
			}
		}
	}
}

func TestGetClientUsesResolvedHostAndAPIVersion(t *testing.T) {
	clearDockerEnvironment(t)

	cli, err := GetClient(CommonArgs{
		DockerHost: pointer("unix:///tmp/dibra-docker.sock"),
		APIVersion: pointer("1.44"),
		Timeout:    pointer(2),
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

	cli, err := GetClient(CommonArgs{DockerHost: pointer("ssh://deploy@docker.example"), UseSSHClient: pointer(true)})
	if err != nil {
		t.Fatalf("GetClient() error = %v", err)
	}
	defer cli.Close()
	if cli.DaemonHost() != "http://docker.example.com" {
		t.Errorf("DaemonHost() = %q, want connhelper host", cli.DaemonHost())
	}
}

func TestGetClientTLSUsesTCPHostWithoutConnecting(t *testing.T) {
	clearDockerEnvironment(t)

	cli, err := GetClient(CommonArgs{
		DockerHost:    pointer("tcp://127.0.0.1:1"),
		TLS:           pointer(true),
		ValidateCerts: pointer(false),
		TLSHostname:   pointer("daemon-tls.ansible.com"),
		Timeout:       pointer(1),
	})
	if err != nil {
		t.Fatalf("GetClient() error = %v", err)
	}
	defer cli.Close()
	if cli.DaemonHost() != "tcp://127.0.0.1:1" {
		t.Errorf("DaemonHost() = %q", cli.DaemonHost())
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

func pointer[T any](value T) *T {
	return &value
}
