package docker

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
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
	DefaultTLS            = false
	DefaultTLSVerify      = false
	DefaultTimeoutSeconds = 60
)

// CommonArgs contains the connection arguments shared by Docker modules.
type CommonArgs struct {
	DockerHost    *string `json:"docker_host"`
	TLS           *bool   `json:"tls"`
	ValidateCerts *bool   `json:"validate_certs"`
	CAPath        *string `json:"ca_path"`
	ClientCert    *string `json:"client_cert"`
	ClientKey     *string `json:"client_key"`
	TLSHostname   *string `json:"tls_hostname"`
	APIVersion    *string `json:"api_version"`
	Timeout       *int    `json:"timeout"`
	CLIContext    *string `json:"cli_context"`
	Debug         *bool   `json:"debug"`
	UseSSHClient  *bool   `json:"use_ssh_client"`
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
	return ResolveConnectionWithEnvironment(args, OSEnvironment{})
}

// ResolveConnectionWithEnvironment is the injectable form of
// ResolveConnection. It implements the API-backed community.docker fallback
// order: explicit module argument, then environment variable, then default.
func ResolveConnectionWithEnvironment(args CommonArgs, environment Environment) (ConnectionOptions, error) {
	if environment == nil {
		environment = OSEnvironment{}
	}
	if (args.ClientCert == nil) != (args.ClientKey == nil) {
		return ConnectionOptions{}, fmt.Errorf("client_cert and client_key must be specified together")
	}

	dockerHost := stringFromArgumentOrEnvironment(args.DockerHost, environment, "DOCKER_HOST", DefaultDockerHost)
	tlsHostname := stringFromArgumentOrEnvironment(args.TLSHostname, environment, "DOCKER_TLS_HOSTNAME", "")
	apiVersion := stringFromArgumentOrEnvironment(args.APIVersion, environment, "DOCKER_API_VERSION", "auto")
	timeout, err := intFromArgumentOrEnvironment(args.Timeout, environment, "DOCKER_TIMEOUT", DefaultTimeoutSeconds)
	if err != nil {
		return ConnectionOptions{}, err
	}
	tlsEnabled, err := boolFromArgumentOrEnvironment(args.TLS, environment, "DOCKER_TLS", DefaultTLS)
	if err != nil {
		return ConnectionOptions{}, err
	}
	validateCerts, err := boolFromArgumentOrEnvironment(args.ValidateCerts, environment, "DOCKER_TLS_VERIFY", DefaultTLSVerify)
	if err != nil {
		return ConnectionOptions{}, err
	}

	result := ConnectionOptions{
		DockerHost:    dockerHost,
		TLS:           tlsEnabled,
		ValidateCerts: validateCerts,
		CAPath:        valueOrDefault(args.CAPath, ""),
		ClientCert:    valueOrDefault(args.ClientCert, ""),
		ClientKey:     valueOrDefault(args.ClientKey, ""),
		TLSHostname:   tlsHostname,
		APIVersion:    apiVersion,
		Timeout:       timeout,
		CLIContext:    valueOrDefault(args.CLIContext, ""),
		Debug:         valueOrDefault(args.Debug, false),
		UseSSHClient:  valueOrDefault(args.UseSSHClient, false),
	}

	if result.DockerHost != "" && result.CLIContext != "" {
		return ConnectionOptions{}, fmt.Errorf("docker_host and cli_context are mutually exclusive")
	}
	if result.ValidateCerts {
		result.TLS = true
	}

	if certDirectory, found := environment.LookupEnv("DOCKER_CERT_PATH"); found {
		if args.CAPath == nil {
			result.CAPath = filepath.Join(certDirectory, "ca.pem")
		}
		if args.ClientCert == nil {
			result.ClientCert = filepath.Join(certDirectory, "cert.pem")
		}
		if args.ClientKey == nil {
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
func GetClient(args CommonArgs) (client.APIClient, error) {
	return GetClientWithEnvironment(args, OSEnvironment{})
}

// GetClientWithEnvironment creates an Engine API client using an injectable
// environment and returns the Moby API interface used by module executors.
func GetClientWithEnvironment(args CommonArgs, environment Environment) (client.APIClient, error) {
	connection, err := ResolveConnectionWithEnvironment(args, environment)
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
	return DockerCLIArgsWithEnvironment(common, OSEnvironment{}, command...)
}

// DockerCLIArgsWithEnvironment is the injectable form of DockerCLIArgs.
func DockerCLIArgsWithEnvironment(common CommonArgs, environment Environment, command ...string) ([]string, error) {
	connection, err := resolveCLIConnection(common, environment)
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
	return DockerCLIEnvWithEnvironment(common, OSEnvironment{})
}

// DockerCLIEnvWithEnvironment returns the child environment after applying
// the same explicit-argument-over-environment precedence used for CLI flags.
func DockerCLIEnvWithEnvironment(common CommonArgs, environment Environment) ([]string, error) {
	connection, err := resolveCLIConnection(common, environment)
	if err != nil {
		return nil, err
	}
	result := withoutEnvironmentKeys(environment.Environ(),
		"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY",
		"DOCKER_CERT_PATH", "DOCKER_API_VERSION", "DOCKER_TLS_HOSTNAME")
	if connection.APIVersion != "auto" {
		result = setEnvironment(result, "DOCKER_API_VERSION", connection.APIVersion)
	}
	if connection.TLSHostname != "" {
		result = setEnvironment(result, "DOCKER_TLS_HOSTNAME", connection.TLSHostname)
	}
	return result, nil
}

func resolveCLIConnection(args CommonArgs, environment Environment) (ConnectionOptions, error) {
	if environment == nil {
		environment = OSEnvironment{}
	}
	if (args.ClientCert == nil) != (args.ClientKey == nil) {
		return ConnectionOptions{}, fmt.Errorf("client_cert and client_key must be specified together")
	}

	dockerHost, dockerHostSupplied := stringFromArgumentOrEnvironmentWithPresence(args.DockerHost, environment, "DOCKER_HOST")
	cliContext := valueOrDefault(args.CLIContext, "")
	if dockerHostSupplied && args.CLIContext != nil {
		return ConnectionOptions{}, fmt.Errorf("docker_host and cli_context are mutually exclusive")
	}
	if dockerHost == "" && cliContext == "" {
		dockerHost = DefaultDockerHost
	}

	tlsEnabled, err := boolFromArgumentOrEnvironment(args.TLS, environment, "DOCKER_TLS", DefaultTLS)
	if err != nil {
		return ConnectionOptions{}, err
	}
	validateCerts, err := boolFromArgumentOrEnvironment(args.ValidateCerts, environment, "DOCKER_TLS_VERIFY", DefaultTLSVerify)
	if err != nil {
		return ConnectionOptions{}, err
	}
	if validateCerts {
		tlsEnabled = true
	}

	connection := ConnectionOptions{
		DockerHost:    dockerHost,
		TLS:           tlsEnabled,
		ValidateCerts: validateCerts,
		CAPath:        valueOrDefault(args.CAPath, ""),
		ClientCert:    valueOrDefault(args.ClientCert, ""),
		ClientKey:     valueOrDefault(args.ClientKey, ""),
		TLSHostname:   stringFromArgumentOrEnvironment(args.TLSHostname, environment, "DOCKER_TLS_HOSTNAME", ""),
		APIVersion:    stringFromArgumentOrEnvironment(args.APIVersion, environment, "DOCKER_API_VERSION", "auto"),
		Timeout:       valueOrDefault(args.Timeout, DefaultTimeoutSeconds),
		CLIContext:    cliContext,
		Debug:         valueOrDefault(args.Debug, false),
		UseSSHClient:  valueOrDefault(args.UseSSHClient, false),
	}

	if certDirectory, found := environment.LookupEnv("DOCKER_CERT_PATH"); found {
		if args.CAPath == nil {
			connection.CAPath = filepath.Join(certDirectory, "ca.pem")
		}
		if args.ClientCert == nil {
			connection.ClientCert = filepath.Join(certDirectory, "cert.pem")
		}
		if args.ClientKey == nil {
			connection.ClientKey = filepath.Join(certDirectory, "key.pem")
		}
	}
	if (connection.ClientCert == "") != (connection.ClientKey == "") {
		return ConnectionOptions{}, fmt.Errorf("client_cert and client_key must be specified together")
	}
	return connection, nil
}

func boolFromArgumentOrEnvironment(argument *bool, environment Environment, name string, defaultValue bool) (bool, error) {
	if argument != nil {
		return *argument, nil
	}
	value, found := environment.LookupEnv(name)
	if !found {
		return defaultValue, nil
	}
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean, got %q", name, value)
	}
}

func intFromArgumentOrEnvironment(argument *int, environment Environment, name string, defaultValue int) (int, error) {
	if argument != nil {
		return *argument, nil
	}
	value, found := environment.LookupEnv(name)
	if !found {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", name, value)
	}
	return parsed, nil
}

func stringFromArgumentOrEnvironment(argument *string, environment Environment, name, defaultValue string) string {
	value, found := stringFromArgumentOrEnvironmentWithPresence(argument, environment, name)
	if found {
		return value
	}
	return defaultValue
}

func stringFromArgumentOrEnvironmentWithPresence(argument *string, environment Environment, name string) (string, bool) {
	if argument != nil {
		return *argument, true
	}
	if value, found := environment.LookupEnv(name); found {
		return value, true
	}
	return "", false
}

func valueOrDefault[T any](argument *T, defaultValue T) T {
	if argument != nil {
		return *argument
	}
	return defaultValue
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
