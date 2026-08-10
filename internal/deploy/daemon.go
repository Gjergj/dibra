package deploy

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gjergjiramku/dibra/internal/agent"
	controller "github.com/gjergjiramku/dibra/internal/controller"
)

const (
	DefaultEndpoint     = "http://localhost:8080/gettasks"
	DefaultPollInterval = 60 * time.Second
	DefaultHTTPTimeout  = 30 * time.Second
	DefaultStateDir     = "/var/lib/dibra-deploy"
	maxArchiveSize      = int64(256 << 20)
)

type Config struct {
	Endpoint         string
	PollInterval     time.Duration
	StateDir         string
	HTTPClient       *http.Client
	AgentMode        agent.Mode
	AgentPath        string
	Version          string
	ProjectRoot      string
	ForceAgentUpload bool
	Verbose          bool
	Stdout           io.Writer
	Stderr           io.Writer
}

type Daemon struct {
	config    Config
	agentPath string
}

func New(config Config) (*Daemon, error) {
	applyDefaults(&config)
	if err := os.MkdirAll(config.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	if err := os.Chmod(config.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure state directory: %w", err)
	}
	jobsDir := filepath.Join(config.StateDir, "jobs")
	if err := os.RemoveAll(jobsDir); err != nil {
		return nil, fmt.Errorf("remove stale job directories: %w", err)
	}
	if err := os.MkdirAll(jobsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create jobs directory: %w", err)
	}

	resolver := agent.NewResolver(agent.Options{
		Mode:        config.AgentMode,
		AgentPath:   config.AgentPath,
		Version:     config.Version,
		ProjectRoot: config.ProjectRoot,
	})
	runtimeAgent := filepath.Join(config.StateDir, "agent", "dibra-agent")
	preparedAgent, err := resolver.PrepareLocal(agent.Target{OS: runtime.GOOS, Arch: runtime.GOARCH}, runtimeAgent, config.Version, config.ForceAgentUpload)
	if err != nil {
		return nil, fmt.Errorf("prepare local agent: %w", err)
	}
	return &Daemon{config: config, agentPath: preparedAgent}, nil
}

func applyDefaults(config *Config) {
	if config.Endpoint == "" {
		config.Endpoint = DefaultEndpoint
	}
	if config.PollInterval <= 0 {
		config.PollInterval = DefaultPollInterval
	}
	if config.StateDir == "" {
		config.StateDir = DefaultStateDir
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
}

func (d *Daemon) Run(ctx context.Context) error {
	for {
		rebooted, err := d.PollOnce(ctx)
		if err != nil && ctx.Err() == nil {
			fmt.Fprintf(d.config.Stderr, "dibra-deploy: %v\n", err)
		}
		if rebooted {
			fmt.Fprintln(d.config.Stdout, "dibra-deploy: local reboot initiated; exiting until the next boot")
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
		timer := time.NewTimer(d.config.PollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (d *Daemon) PollOnce(ctx context.Context) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, d.config.Endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("create task request: %w", err)
	}
	response, err := d.config.HTTPClient.Do(request)
	if err != nil {
		return false, fmt.Errorf("fetch tasks: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNoContent {
		fmt.Fprintln(d.config.Stdout, "dibra-deploy: no work available")
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return false, fmt.Errorf("task server returned HTTP %d (%s)", response.StatusCode, response.Status)
	}
	if response.ContentLength > maxArchiveSize {
		return false, fmt.Errorf("task archive is %d bytes; maximum is %d", response.ContentLength, maxArchiveSize)
	}

	jobDir, err := os.MkdirTemp(filepath.Join(d.config.StateDir, "jobs"), "job-*")
	if err != nil {
		return false, fmt.Errorf("create job directory: %w", err)
	}
	defer os.RemoveAll(jobDir)
	archivePath := filepath.Join(jobDir, "project.zip")
	hash, err := saveArchive(response.Body, archivePath)
	if err != nil {
		return false, err
	}
	fmt.Fprintf(d.config.Stdout, "dibra-deploy: received job %x\n", hash[:8])

	project, err := ExtractProject(archivePath, filepath.Join(jobDir, "project"))
	if err != nil {
		return false, fmt.Errorf("validate task archive: %w", err)
	}
	return d.executeProject(ctx, project)
}

func saveArchive(input io.Reader, archivePath string) ([32]byte, error) {
	var empty [32]byte
	output, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return empty, fmt.Errorf("create task archive: %w", err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hasher), io.LimitReader(input, maxArchiveSize+1))
	closeErr := output.Close()
	if copyErr != nil {
		return empty, fmt.Errorf("download task archive: %w", copyErr)
	}
	if closeErr != nil {
		return empty, fmt.Errorf("close task archive: %w", closeErr)
	}
	if written > maxArchiveSize {
		return empty, fmt.Errorf("task archive exceeds %d bytes", maxArchiveSize)
	}
	var sum [32]byte
	copy(sum[:], hasher.Sum(nil))
	return sum, nil
}

func (d *Daemon) executeProject(ctx context.Context, project Project) (bool, error) {
	if err := validateRebootPlacement(project); err != nil {
		return false, fmt.Errorf("deployment preflight failed: %w", err)
	}
	for index, playbook := range project.Manifest.Playbooks {
		finalPlaybook := index == len(project.Manifest.Playbooks)-1
		playbookPath := filepath.Join(project.Root, filepath.FromSlash(playbook))
		fmt.Fprintf(d.config.Stdout, "dibra-deploy: executing %s\n", playbook)
		result, err := controller.Run(ctx, controller.RunOptions{
			ConfigPath:     playbookPath,
			Version:        d.config.Version,
			Local:          true,
			LocalAgentPath: d.agentPath,
			WorkingDir:     project.Root,
			AllowReboot:    finalPlaybook,
			Verbose:        d.config.Verbose,
			Stdout:         d.config.Stdout,
			Stderr:         d.config.Stderr,
		})
		if err != nil {
			return false, fmt.Errorf("execute playbook %q: %w", playbook, err)
		}
		if result.Failed {
			return false, fmt.Errorf("playbook %q failed", playbook)
		}
		if result.RebootInitiated {
			if !finalPlaybook {
				return false, fmt.Errorf("playbook %q initiated reboot before the final playbook", playbook)
			}
			return true, nil
		}
	}
	fmt.Fprintln(d.config.Stdout, "dibra-deploy: job completed successfully")
	return false, nil
}
