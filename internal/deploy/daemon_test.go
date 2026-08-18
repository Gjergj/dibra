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

func TestPollOnceReportsSuccessfulTaskOutcome(t *testing.T) {
	t.Parallel()
	archive := deploymentZIP(t, "tasks:\n  - name: ping locally\n    ping:\n")
	var reported taskOutcome
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.Method {
		case http.MethodGet:
			return taskResponse(http.StatusOK, archive, "task-123", request), nil
		case http.MethodPost:
			if request.URL.Path != "/gettasks_outcome" {
				t.Fatalf("outcome path = %q", request.URL.Path)
			}
			if err := json.NewDecoder(request.Body).Decode(&reported); err != nil {
				t.Fatal(err)
			}
			return response(http.StatusNoContent, nil, request), nil
		default:
			t.Fatalf("unexpected request method %s", request.Method)
			return nil, nil
		}
	})}

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
	if rebooted, err := daemon.PollOnce(context.Background()); err != nil || rebooted {
		t.Fatalf("PollOnce() = rebooted %v, error %v", rebooted, err)
	}
	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "run"); got != 1 {
		t.Fatalf("agent ran %d times, want 1", got)
	}
	if reported.TaskID != "task-123" || reported.Status != "succeeded" || reported.Error != "" || reported.RebootInitiated {
		t.Fatalf("reported outcome = %#v", reported)
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("job directories were not cleaned up: %#v", entries)
	}
}

func TestPollOnceReportsFailedTaskOutcome(t *testing.T) {
	t.Parallel()
	var reported taskOutcome
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet {
			return taskResponse(http.StatusOK, []byte("not a ZIP"), "bad-archive", request), nil
		}
		if err := json.NewDecoder(request.Body).Decode(&reported); err != nil {
			t.Fatal(err)
		}
		return response(http.StatusNoContent, nil, request), nil
	})}
	stateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{config: Config{
		Endpoint: "http://localhost/gettasks", HTTPClient: client, StateDir: stateDir,
		Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	}}

	_, err := daemon.PollOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "validate task archive") {
		t.Fatalf("PollOnce() error = %v", err)
	}
	if reported.TaskID != "bad-archive" || reported.Status != "failed" || !strings.Contains(reported.Error, "validate task archive") {
		t.Fatalf("reported outcome = %#v", reported)
	}
}

func TestPollOnceRequiresTaskID(t *testing.T) {
	t.Parallel()
	daemon := &Daemon{config: Config{
		Endpoint: "http://localhost/gettasks", HTTPClient: responseClient(http.StatusOK, deploymentZIP(t, "tasks: []\n")),
		StateDir: t.TempDir(), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	}}
	_, err := daemon.PollOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), TaskIDHeader) {
		t.Fatalf("PollOnce() error = %v", err)
	}
}

func TestDeriveOutcomeEndpoint(t *testing.T) {
	t.Parallel()
	actual, err := deriveOutcomeEndpoint("http://host:8080/api/gettasks?token=test")
	if err != nil {
		t.Fatal(err)
	}
	if actual != "http://host:8080/api/gettasks_outcome" {
		t.Fatalf("outcome endpoint = %q", actual)
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

func taskResponse(status int, body []byte, taskID string, request *http.Request) *http.Response {
	result := response(status, body, request)
	result.Header.Set(TaskIDHeader, taskID)
	return result
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
