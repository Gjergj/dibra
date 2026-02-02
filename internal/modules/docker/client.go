package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/tlsconfig"
)

const (
	DefaultDockerHost     = "unix:///var/run/docker.sock"
	DefaultTLSVerify      = false
	DefaultTimeoutSeconds = 60
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

// GetContainerUser returns the user configured for the container
func GetContainerUser(ctx context.Context, cli *client.Client, containerID string) (string, error) {
	inspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", err
	}
	user := inspect.Config.User
	if user == "" {
		return "root", nil
	}
	return user, nil
}

// GetContainerUserIDs returns the numeric UID and GID for a given user in the container
func GetContainerUserIDs(ctx context.Context, cli *client.Client, containerID string, user string) (uid, gid int, err error) {
	if user == "" {
		user = "root"
	}

	execConfig := types.ExecConfig{
		Cmd:          []string{"sh", "-c", "id -u && id -g"},
		AttachStdout: true,
		AttachStderr: true,
		User:         user,
	}

	execResp, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return 0, 0, err
	}

	hijack, err := cli.ContainerExecAttach(ctx, execResp.ID, types.ExecStartCheck{})
	if err != nil {
		return 0, 0, err
	}
	defer hijack.Close()

	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, hijack.Reader)
	if err != nil && err != io.EOF {
		return 0, 0, err
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) < 2 {
		return 0, 0, fmt.Errorf("failed to get UID/GID, output: %q", stdout.String())
	}

	uid, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse UID: %v", err)
	}

	gid, err = strconv.Atoi(strings.TrimSpace(lines[1]))
	if err != nil {
		return uid, 0, fmt.Errorf("failed to parse GID: %v", err)
	}

	return uid, gid, nil
}

// Ping checks connectivity
func Ping(ctx context.Context, cli *client.Client) error {
	_, err := cli.Ping(ctx)
	return err
}
