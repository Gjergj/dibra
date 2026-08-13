package docker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/moby/moby/client"
)

// Environment provides the process environment used while resolving Docker
// connection options. Keeping it injectable makes option precedence tests
// independent from the agent process environment.
type Environment interface {
	LookupEnv(string) (string, bool)
	Environ() []string
}

// OSEnvironment reads the current process environment.
type OSEnvironment struct{}

func (OSEnvironment) LookupEnv(key string) (string, bool) { return os.LookupEnv(key) }
func (OSEnvironment) Environ() []string                   { return os.Environ() }

// StaticEnvironment is a deterministic environment useful to unit tests.
type StaticEnvironment map[string]string

func (environment StaticEnvironment) LookupEnv(key string) (string, bool) {
	value, found := environment[key]
	return value, found
}

func (environment StaticEnvironment) Environ() []string {
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+environment[key])
	}
	return result
}

// FileSystem contains the filesystem operations used by Docker executors.
// Tests can provide an in-memory or recording implementation.
type FileSystem interface {
	Stat(string) (fs.FileInfo, error)
	ReadFile(string) ([]byte, error)
	Readlink(string) (string, error)
	Open(string) (io.ReadCloser, error)
	Create(string) (io.WriteCloser, error)
	MkdirAll(string, fs.FileMode) error
	WriteFile(string, []byte, fs.FileMode) error
	UserHomeDir() (string, error)
	Abs(string) (string, error)
	EvalSymlinks(string) (string, error)
	WalkDir(string, fs.WalkDirFunc) error
}

// OSFileSystem delegates filesystem operations to the operating system.
type OSFileSystem struct{}

func (OSFileSystem) Stat(path string) (fs.FileInfo, error) { return os.Stat(path) }
func (OSFileSystem) ReadFile(path string) ([]byte, error)  { return os.ReadFile(path) }
func (OSFileSystem) Readlink(path string) (string, error)  { return os.Readlink(path) }
func (OSFileSystem) Open(path string) (io.ReadCloser, error) {
	return os.Open(path)
}
func (OSFileSystem) Create(path string) (io.WriteCloser, error) { return os.Create(path) }
func (OSFileSystem) MkdirAll(path string, mode fs.FileMode) error {
	return os.MkdirAll(path, mode)
}
func (OSFileSystem) WriteFile(path string, data []byte, mode fs.FileMode) error {
	return os.WriteFile(path, data, mode)
}
func (OSFileSystem) UserHomeDir() (string, error) { return os.UserHomeDir() }
func (OSFileSystem) Abs(path string) (string, error) {
	return filepath.Abs(path)
}
func (OSFileSystem) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
func (OSFileSystem) WalkDir(root string, walk fs.WalkDirFunc) error {
	return filepath.WalkDir(root, walk)
}

// Clock contains the wall-clock operations used by Docker executors.
type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
}

// SystemClock delegates to the process wall clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time            { return time.Now() }
func (SystemClock) Sleep(delay time.Duration) { time.Sleep(delay) }

// CLICommand describes one Docker CLI invocation.
type CLICommand struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin io.Reader
}

// CLIResult records combined stdout/stderr and the process exit code. ExitCode
// is -1 when the process could not be started.
type CLIResult struct {
	Output   []byte
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// CLIRunner executes Docker CLI commands.
type CLIRunner interface {
	Run(context.Context, CLICommand) (CLIResult, error)
}

// ExecCLIRunner executes commands through os/exec.
type ExecCLIRunner struct{}

func (ExecCLIRunner) Run(ctx context.Context, command CLICommand) (CLIResult, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = command.Env
	cmd.Stdin = command.Stdin

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := append(append([]byte(nil), stdout.Bytes()...), stderr.Bytes()...)
	result := CLIResult{Output: output, Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: 0}
	if err == nil {
		return result, nil
	}

	result.ExitCode = -1
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
	}
	return result, err
}

// ClientFactory creates an Engine API client for a module invocation.
type ClientFactory func(CommonArgs) (client.APIClient, error)

// Dependencies contains the external effects used by Docker executors. The
// zero value is valid; Resolve fills omitted dependencies with production
// implementations.
type Dependencies struct {
	Environment Environment
	NewClient   ClientFactory
	FileSystem  FileSystem
	Clock       Clock
	CLIRunner   CLIRunner
}

// Resolve fills omitted dependencies with production implementations.
func (dependencies Dependencies) Resolve() Dependencies {
	if dependencies.Environment == nil {
		dependencies.Environment = OSEnvironment{}
	}
	if dependencies.NewClient == nil {
		environment := dependencies.Environment
		dependencies.NewClient = func(args CommonArgs) (client.APIClient, error) {
			return GetClientWithEnvironment(args, environment)
		}
	}
	if dependencies.FileSystem == nil {
		dependencies.FileSystem = OSFileSystem{}
	}
	if dependencies.Clock == nil {
		dependencies.Clock = SystemClock{}
	}
	if dependencies.CLIRunner == nil {
		dependencies.CLIRunner = ExecCLIRunner{}
	}
	return dependencies
}
