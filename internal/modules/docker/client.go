package docker

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/docker/docker/client"
	"github.com/docker/go-connections/tlsconfig"
)

const (
	DefaultDockerHost      = "unix:///var/run/docker.sock"
	DefaultTLSVerify       = false
	DefaultTimeoutSeconds  = 60
)

// CommonArgs common arguments for all Docker modules
type CommonArgs struct {
	DockerHost    string `json:"docker_host"`
	TLS           bool   `json:"tls"`
	ValidateCerts bool   `json:"validate_certs"`
	CAPath        string `json:"ca_path"`
	ClientCert    string `json:"client_cert"`
	ClientKey     string `json:"client_key"`
	APIVersion    string `json:"api_version"`
	Timeout       int    `json:"timeout"`
	Debug         bool   `json:"debug"`
}

// GetClient creates a new Docker client based on arguments and environment variables
func GetClient(args CommonArgs) (*client.Client, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	host := args.DockerHost
	if host == "" {
		host = os.Getenv("DOCKER_HOST")
	}
	if host == "" {
		host = DefaultDockerHost
	}
	opts = append(opts, client.WithHost(host))

	// TLS Configuration
	useTLS := args.TLS || args.ValidateCerts
	if !useTLS {
		if val, err := strconv.ParseBool(os.Getenv("DOCKER_TLS")); err == nil && val {
			useTLS = true
		}
		if val, err := strconv.ParseBool(os.Getenv("DOCKER_TLS_VERIFY")); err == nil && val {
			useTLS = true
		}
	}

	if useTLS {
		verify := args.ValidateCerts
		if !verify {
			if val, err := strconv.ParseBool(os.Getenv("DOCKER_TLS_VERIFY")); err == nil {
				verify = val
			}
		}

		// Cert paths
		caPath := args.CAPath
		certPath := args.ClientCert
		keyPath := args.ClientKey

		envCertPath := os.Getenv("DOCKER_CERT_PATH")
		if envCertPath != "" {
			if caPath == "" {
				caPath = filepath.Join(envCertPath, "ca.pem")
			}
			if certPath == "" {
				certPath = filepath.Join(envCertPath, "cert.pem")
			}
			if keyPath == "" {
				keyPath = filepath.Join(envCertPath, "key.pem")
			}
		}

		tlsOption := tlsconfig.Options{
			CAFile:             caPath,
			CertFile:           certPath,
			KeyFile:            keyPath,
			InsecureSkipVerify: !verify,
		}

		tlsc, err := tlsconfig.Client(tlsOption)
		if err != nil {
			return nil, err
		}

		httpClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsc,
			},
		}
		opts = append(opts, client.WithHTTPClient(httpClient))
	}

	if args.APIVersion != "" && args.APIVersion != "auto" {
		opts = append(opts, client.WithVersion(args.APIVersion))
	} else if os.Getenv("DOCKER_API_VERSION") != "" {
		opts = append(opts, client.WithVersion(os.Getenv("DOCKER_API_VERSION")))
	}

	return client.NewClientWithOpts(opts...)
}

// Ping checks connectivity
func Ping(ctx context.Context, cli *client.Client) error {
	_, err := cli.Ping(ctx)
	return err
}
