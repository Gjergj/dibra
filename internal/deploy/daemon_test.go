package deploy

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/agent"
	"github.com/gjergjiramku/dibra/internal/execution"
)

func TestNewPreparesAgentAndRemovesStaleJobs(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	stateDir := filepath.Join(temporary, "state")
	stalePath := filepath.Join(stateDir, "jobs", "stale-job", "old")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceAgent := filepath.Join(temporary, "agent")
	if err := os.WriteFile(sourceAgent, []byte("#!/bin/sh\necho 'dibra-agent dev (commit: none, built: unknown)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	daemon, err := New(Config{
		StateDir: stateDir, AgentMode: agent.ModePath, AgentPath: sourceAgent, Version: "dev",
		HTTPClient: responseClient(http.StatusNoContent, nil), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if daemon.agentPath != filepath.Join(stateDir, "agent", "dibra-agent") {
		t.Fatalf("runtime agent path = %q", daemon.agentPath)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale job still exists: %v", err)
	}
	info, err := os.Stat(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("state directory mode = %o", info.Mode().Perm())
	}
}

func TestPollOnceHandlesNoContent(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	client := responseClient(http.StatusNoContent, nil)
	daemon := &Daemon{config: Config{Endpoint: "http://localhost/gettasks", HTTPClient: client, StateDir: t.TempDir(), Stdout: &stdout, Stderr: &bytes.Buffer{}}}
	if rebooted, err := daemon.PollOnce(context.Background()); err != nil || rebooted {
		t.Fatalf("PollOnce() = rebooted %v, error %v", rebooted, err)
	}
	if !strings.Contains(stdout.String(), "no work") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestPollOnceExecutesEveryHTTP200(t *testing.T) {
	t.Parallel()
	archive := deploymentZIP(t, "tasks:\n  - name: ping locally\n    ping:\n")
	client := responseClient(http.StatusOK, archive)

	temporary := t.TempDir()
	counter := filepath.Join(temporary, "agent-count")
	agentPath := filepath.Join(temporary, "fake-agent")
	script := "#!/bin/sh\ncat >/dev/null\necho run >> '" + counter + "'\necho '{\"changed\":false,\"failed\":false,\"ping\":\"pong\"}'\n"
	if err := os.WriteFile(agentPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(temporary, "state")
	if err := os.MkdirAll(filepath.Join(stateDir, "jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{
		config:    Config{Endpoint: "http://localhost/gettasks", HTTPClient: client, StateDir: stateDir, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}},
		agentPath: agentPath,
	}
	for index := 0; index < 2; index++ {
		if rebooted, err := daemon.PollOnce(context.Background()); err != nil || rebooted {
			t.Fatalf("PollOnce() attempt %d = rebooted %v, error %v", index, rebooted, err)
		}
	}
	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "run"); got != 2 {
		t.Fatalf("agent ran %d times, want 2", got)
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("job directories were not cleaned up: %#v", entries)
	}
}

func TestExecuteProjectRunsPlaybooksInOrderAndStopsOnFailure(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	requestLog := filepath.Join(temporary, "requests.log")
	agentPath := filepath.Join(temporary, "fake-agent")
	script := "#!/bin/sh\ninput=$(cat)\nprintf '%s\\n' \"$input\" >> '" + requestLog + "'\n" +
		"case \"$input\" in\n  *crash*) echo '{\"changed\":false,\"failed\":true,\"msg\":\"intentional\"}' ;;\n  *) echo '{\"changed\":false,\"failed\":false,\"ping\":\"pong\"}' ;;\nesac\n"
	if err := os.WriteFile(agentPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	playbooks := []string{"first.yaml", "second.yaml", "third.yaml"}
	for index, value := range []string{"first", "crash", "third"} {
		contents := "tasks:\n  - name: " + value + "\n    ping:\n      data: " + value + "\n"
		if err := os.WriteFile(filepath.Join(temporary, playbooks[index]), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	daemon := &Daemon{
		config:    Config{Version: "dev", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}},
		agentPath: agentPath,
	}
	_, err := daemon.executeProject(context.Background(), Project{
		Root: temporary, Manifest: Manifest{Version: 1, Playbooks: playbooks},
	})
	if err == nil || !strings.Contains(err.Error(), "second.yaml") {
		t.Fatalf("executeProject() error = %v", err)
	}
	requests, readErr := os.ReadFile(requestLog)
	if readErr != nil {
		t.Fatal(readErr)
	}
	lines := strings.Split(strings.TrimSpace(string(requests)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "first") || !strings.Contains(lines[1], "crash") {
		t.Fatalf("agent requests = %#v", lines)
	}
	for index, line := range lines {
		var request execution.ModuleRequest[json.RawMessage]
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			t.Fatalf("decode agent request %d: %v", index, err)
		}
		if request.State != (execution.State{}) {
			t.Fatalf("deploy agent request %d state = %#v, want default state", index, request.State)
		}
	}
}

func TestPollOnceReportsHTTPFailure(t *testing.T) {
	t.Parallel()
	daemon := &Daemon{config: Config{Endpoint: "http://localhost/gettasks", HTTPClient: responseClient(http.StatusServiceUnavailable, []byte("broken")), StateDir: t.TempDir(), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}}
	_, err := daemon.PollOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("PollOnce() error = %v", err)
	}
}

func TestRunRecoversAfterHTTPFailureAndStopsOnCancellation(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			return response(http.StatusServiceUnavailable, []byte("retry"), request), nil
		}
		cancel()
		return response(http.StatusNoContent, nil, request), nil
	})}
	var stdout, stderr bytes.Buffer
	daemon := &Daemon{config: Config{
		Endpoint: "http://localhost/gettasks", PollInterval: time.Millisecond,
		HTTPClient: client, StateDir: t.TempDir(), Stdout: &stdout, Stderr: &stderr,
	}}
	if err := daemon.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("HTTP attempts = %d, want 2", attempts.Load())
	}
	if !strings.Contains(stderr.String(), "HTTP 503") || !strings.Contains(stdout.String(), "no work") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestPollOnceHonorsHTTPTimeout(t *testing.T) {
	t.Parallel()
	client := &http.Client{
		Timeout: 10 * time.Millisecond,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}),
	}
	daemon := &Daemon{config: Config{
		Endpoint: "http://localhost/gettasks", HTTPClient: client, StateDir: t.TempDir(),
		Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	}}
	started := time.Now()
	_, err := daemon.PollOnce(context.Background())
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PollOnce() error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("HTTP timeout took %s", elapsed)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func responseClient(status int, body []byte) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return response(status, body, request), nil
	})}
}

func response(status int, body []byte, request *http.Request) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Header:        make(http.Header),
		Request:       request,
	}
}

func deploymentZIP(t *testing.T, playbook string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range map[string]string{
		manifestName:    "version: 1\nplaybooks:\n  - playbook.yaml\n",
		"playbook.yaml": playbook,
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
