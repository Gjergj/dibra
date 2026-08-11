package docker

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/cli/cli/connhelper"
	"github.com/docker/go-connections/tlsconfig"
	"github.com/moby/moby/client"
)

const (
	DefaultDockerHost     = "unix:///var/run/docker.sock"
	DefaultTLSVerify      = false
	DefaultTimeoutSeconds = 60
)

// CommonArgs contains the connection arguments shared by Docker modules.
type CommonArgs struct {
	DockerHost    string `json:"docker_host"`
	TLS           bool   `json:"tls"`
	ValidateCerts bool   `json:"validate_certs"`
	CAPath        string `json:"ca_path"`
	ClientCert    string `json:"client_cert"`
	ClientKey     string `json:"client_key"`
	TLSHostname   string `json:"tls_hostname"`
	APIVersion    string `json:"api_version"`
	Timeout       int    `json:"timeout"`
	CLIContext    string `json:"cli_context"`
	Debug         bool   `json:"debug"`
	UseSSHClient  bool   `json:"use_ssh_client"`
}

// ConnectionOptions is the normalized connection configuration used by both
// API-backed modules and modules which invoke the Docker CLI.
type ConnectionOptions struct {
	DockerHost    string
	TLS           bool
	ValidateCerts bool
	CAPath        string
	ClientCert    string
	ClientKey     string
	TLSHostname   string
	APIVersion    string
	Timeout       int
	CLIContext    string
	Debug         bool
	UseSSHClient  bool
}

// ResolveConnection applies community.docker-compatible environment fallbacks
// and validates options which are shared by every Docker transport.
func ResolveConnection(args CommonArgs) (ConnectionOptions, error) {
	result := ConnectionOptions{
		DockerHost:    firstNonEmpty(args.DockerHost, os.Getenv("DOCKER_HOST")),
		TLS:           args.TLS,
		ValidateCerts: args.ValidateCerts,
		CAPath:        args.CAPath,
		ClientCert:    args.ClientCert,
		ClientKey:     args.ClientKey,
		TLSHostname:   firstNonEmpty(args.TLSHostname, os.Getenv("DOCKER_TLS_HOSTNAME")),
		APIVersion:    firstNonEmpty(args.APIVersion, os.Getenv("DOCKER_API_VERSION")),
		Timeout:       args.Timeout,
		CLIContext:    args.CLIContext,
		Debug:         args.Debug,
		UseSSHClient:  args.UseSSHClient,
	}

	if result.DockerHost != "" && result.CLIContext != "" {
		return ConnectionOptions{}, fmt.Errorf("docker_host and cli_context are mutually exclusive")
	}
	if result.DockerHost == "" && result.CLIContext == "" {
		result.DockerHost = DefaultDockerHost
	}
	if result.APIVersion == "" {
		result.APIVersion = "auto"
	}

	if result.Timeout <= 0 {
		if value := os.Getenv("DOCKER_TIMEOUT"); value != "" {
			timeout, err := strconv.Atoi(value)
			if err != nil || timeout <= 0 {
				return ConnectionOptions{}, fmt.Errorf("DOCKER_TIMEOUT must be a positive integer, got %q", value)
			}
			result.Timeout = timeout
		} else {
			result.Timeout = DefaultTimeoutSeconds
		}
	}

	var err error
	if !result.TLS {
		result.TLS, err = boolFromEnv("DOCKER_TLS")
		if err != nil {
			return ConnectionOptions{}, err
		}
	}
	if !result.ValidateCerts {
		result.ValidateCerts, err = boolFromEnv("DOCKER_TLS_VERIFY")
		if err != nil {
			return ConnectionOptions{}, err
		}
	}
	if result.ValidateCerts {
		result.TLS = true
	}

	if certDirectory := os.Getenv("DOCKER_CERT_PATH"); certDirectory != "" {
		if result.CAPath == "" {
			result.CAPath = filepath.Join(certDirectory, "ca.pem")
		}
		if result.ClientCert == "" {
			result.ClientCert = filepath.Join(certDirectory, "cert.pem")
		}
		if result.ClientKey == "" {
			result.ClientKey = filepath.Join(certDirectory, "key.pem")
		}
	}
	if (result.ClientCert == "") != (result.ClientKey == "") {
		return ConnectionOptions{}, fmt.Errorf("client_cert and client_key must be specified together")
	}

	if result.TLSHostname == "" && result.DockerHost != "" {
		parsed, parseErr := url.Parse(result.DockerHost)
		if parseErr == nil {
			result.TLSHostname = parsed.Hostname()
		}
	}

	return result, nil
}

// GetClient creates an Engine API client from the shared connection options.
func GetClient(args CommonArgs) (*client.Client, error) {
	connection, err := ResolveConnection(args)
	if err != nil {
		return nil, err
	}
	if connection.CLIContext != "" {
		return nil, fmt.Errorf("cli_context is only supported by Docker CLI-backed modules")
	}

	var clientOptions []client.Opt
	if strings.HasPrefix(connection.DockerHost, "ssh://") {
		if connection.TLS {
			return nil, fmt.Errorf("TLS options cannot be combined with an ssh:// docker_host")
		}
		helper, helperErr := connhelper.GetConnectionHelper(connection.DockerHost)
		if helperErr != nil {
			return nil, helperErr
		}
		if helper == nil {
			return nil, fmt.Errorf("unsupported Docker connection helper for %q", connection.DockerHost)
		}
		clientOptions = append(clientOptions, client.WithHost(helper.Host), client.WithDialContext(helper.Dialer))
	} else {
		if connection.TLS {
			tlsConfig, tlsErr := newTLSConfig(connection)
			if tlsErr != nil {
				return nil, tlsErr
			}
			httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
			// WithHost configures the transport, so it must follow WithHTTPClient.
			clientOptions = append(clientOptions, client.WithHTTPClient(httpClient))
		}
		clientOptions = append(clientOptions, client.WithHost(connection.DockerHost))
	}

	// Apply timeout after any custom HTTP client so it cannot be overwritten.
	clientOptions = append(clientOptions, client.WithTimeout(time.Duration(connection.Timeout)*time.Second))
	if connection.APIVersion != "auto" {
		clientOptions = append(clientOptions, client.WithAPIVersion(connection.APIVersion))
	}
	return client.New(clientOptions...)
}

func newTLSConfig(connection ConnectionOptions) (*tls.Config, error) {
	tlsConfig, err := tlsconfig.Client(tlsconfig.Options{
		CAFile:             connection.CAPath,
		CertFile:           connection.ClientCert,
		KeyFile:            connection.ClientKey,
		InsecureSkipVerify: !connection.ValidateCerts,
		ExclusiveRootPools: true,
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		return nil, err
	}
	tlsConfig.ServerName = connection.TLSHostname
	return tlsConfig, nil
}

// DockerCLIArgs prepends normalized global Docker CLI connection flags.
func DockerCLIArgs(common CommonArgs, command ...string) ([]string, error) {
	connection, err := ResolveConnection(common)
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, len(command)+12)
	if connection.DockerHost != "" {
		args = append(args, "--host", connection.DockerHost)
	}
	if connection.ValidateCerts {
		args = append(args, "--tlsverify")
	} else if connection.TLS {
		args = append(args, "--tls")
	}
	if connection.CAPath != "" {
		args = append(args, "--tlscacert", connection.CAPath)
	}
	if connection.ClientCert != "" {
		args = append(args, "--tlscert", connection.ClientCert)
	}
	if connection.ClientKey != "" {
		args = append(args, "--tlskey", connection.ClientKey)
	}
	if connection.CLIContext != "" {
		args = append(args, "--context", connection.CLIContext)
	}
	if connection.Debug {
		args = append(args, "--debug")
	}
	return append(args, command...), nil
}

// DockerCLIEnv returns a deterministic environment for Docker CLI-backed modules.
func DockerCLIEnv(common CommonArgs) ([]string, error) {
	connection, err := ResolveConnection(common)
	if err != nil {
		return nil, err
	}
	environment := withoutEnvironmentKeys(os.Environ(),
		"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY",
		"DOCKER_CERT_PATH", "DOCKER_API_VERSION", "DOCKER_TLS_HOSTNAME")
	if connection.APIVersion != "auto" {
		environment = setEnvironment(environment, "DOCKER_API_VERSION", connection.APIVersion)
	}
	if connection.TLSHostname != "" {
		environment = setEnvironment(environment, "DOCKER_TLS_HOSTNAME", connection.TLSHostname)
	}
	return environment, nil
}

func boolFromEnv(name string) (bool, error) {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if value == "" {
		return false, nil
	}
	switch value {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean, got %q", name, value)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func withoutEnvironmentKeys(environment []string, keys ...string) []string {
	excluded := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		excluded[key] = struct{}{}
	}
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if _, found := excluded[key]; !found {
			result = append(result, entry)
		}
	}
	return result
}

func setEnvironment(environment []string, key, value string) []string {
	environment = withoutEnvironmentKeys(environment, key)
	return append(environment, key+"="+value)
}
