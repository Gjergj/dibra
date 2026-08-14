package docker_container_exec

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

type execClient struct {
	client.APIClient

	createOptions client.ExecCreateOptions
	startOptions  client.ExecStartOptions
	attachOptions client.ExecAttachOptions
	inspectResult client.ExecInspectResult
	execID        string
	stdout        string
	stderr        string
	systemError   string
	createErr     error
	startErr      error
	attachErr     error
	inspectErr    error
	connection    *recordingConn
	closed        bool
}

func (fake *execClient) ExecCreate(_ context.Context, _ string, options client.ExecCreateOptions) (client.ExecCreateResult, error) {
	fake.createOptions = options
	return client.ExecCreateResult{ID: fake.execID}, fake.createErr
}

func (fake *execClient) ExecStart(_ context.Context, _ string, options client.ExecStartOptions) (client.ExecStartResult, error) {
	fake.startOptions = options
	return client.ExecStartResult{}, fake.startErr
}

func (fake *execClient) ExecAttach(_ context.Context, _ string, options client.ExecAttachOptions) (client.ExecAttachResult, error) {
	fake.attachOptions = options
	if fake.attachErr != nil {
		return client.ExecAttachResult{}, fake.attachErr
	}
	fake.connection = &recordingConn{}
	output := []byte(fake.stdout)
	if !options.TTY {
		var multiplexed bytes.Buffer
		writeMultiplexedFrame(&multiplexed, stdcopy.Stdout, fake.stdout)
		writeMultiplexedFrame(&multiplexed, stdcopy.Stderr, fake.stderr)
		if fake.systemError != "" {
			writeMultiplexedFrame(&multiplexed, stdcopy.Systemerr, fake.systemError)
		}
		output = multiplexed.Bytes()
	}
	return client.ExecAttachResult{HijackedResponse: client.HijackedResponse{
		Conn:   fake.connection,
		Reader: bufio.NewReader(bytes.NewReader(output)),
	}}, nil
}

func writeMultiplexedFrame(destination *bytes.Buffer, stream stdcopy.StdType, value string) {
	header := make([]byte, 8)
	header[0] = byte(stream)
	binary.BigEndian.PutUint32(header[4:], uint32(len(value)))
	_, _ = destination.Write(header)
	_, _ = destination.WriteString(value)
}

func (fake *execClient) ExecInspect(_ context.Context, _ string, _ client.ExecInspectOptions) (client.ExecInspectResult, error) {
	return fake.inspectResult, fake.inspectErr
}

func (fake *execClient) Close() error {
	fake.closed = true
	return nil
}

type recordingConn struct {
	mu          sync.Mutex
	written     bytes.Buffer
	writeClosed bool
	closed      bool
}

func (connection *recordingConn) Read([]byte) (int, error) { return 0, io.EOF }

func (connection *recordingConn) Write(data []byte) (int, error) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return connection.written.Write(data)
}

func (connection *recordingConn) CloseWrite() error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	connection.writeClosed = true
	return nil
}

func (connection *recordingConn) Close() error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	connection.closed = true
	return nil
}

func (*recordingConn) LocalAddr() net.Addr              { return &net.IPAddr{} }
func (*recordingConn) RemoteAddr() net.Addr             { return &net.IPAddr{} }
func (*recordingConn) SetDeadline(time.Time) error      { return nil }
func (*recordingConn) SetReadDeadline(time.Time) error  { return nil }
func (*recordingConn) SetWriteDeadline(time.Time) error { return nil }
func boolPointer(value bool) *bool                      { return &value }
func stringPointer(value string) *string                { return &value }
func responseString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func responseInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
func fakeDependencies(fake *execClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
	}
}

func TestExecuteMatchesCommandOutputAndOptionContract(t *testing.T) {
	fake := &execClient{
		execID:        "exec-1",
		stdout:        "hello world\n\n",
		stderr:        "warning\n",
		inspectResult: client.ExecInspectResult{ExitCode: 7},
	}
	response := ExecuteWithDependencies(Request{
		Container:  "web",
		Command:    `/bin/sh -c "printf 'hello world'"`,
		Chdir:      "/work",
		User:       "1000",
		Privileged: true,
		Env:        map[string]any{"ZED": "last", "ALPHA": "first"},
	}, fakeDependencies(fake))

	if response.Failed || !response.Changed || responseInt(response.RC) != 7 {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if responseString(response.Stdout) != "hello world" || responseString(response.Stderr) != "warning" {
		t.Fatalf("output = stdout %q, stderr %q", responseString(response.Stdout), responseString(response.Stderr))
	}
	wantCommand := []string{"/bin/sh", "-c", "printf 'hello world'"}
	if strings.Join(fake.createOptions.Cmd, "\x00") != strings.Join(wantCommand, "\x00") {
		t.Fatalf("command = %#v, want %#v", fake.createOptions.Cmd, wantCommand)
	}
	if fake.createOptions.WorkingDir != "/work" || fake.createOptions.User != "1000" || !fake.createOptions.Privileged {
		t.Fatalf("create options = %#v", fake.createOptions)
	}
	if strings.Join(fake.createOptions.Env, ",") != "ALPHA=first,ZED=last" {
		t.Fatalf("environment = %#v", fake.createOptions.Env)
	}
	if !fake.closed {
		t.Fatal("injected API client was not closed")
	}
}

func TestExecuteWritesLongStdinConcurrentlyAndClosesWriteSide(t *testing.T) {
	stdin := strings.Repeat("long input ", 20000)
	fake := &execClient{
		execID:        "exec-stdin",
		stdout:        "done\n",
		inspectResult: client.ExecInspectResult{ExitCode: 0},
	}
	response := ExecuteWithDependencies(Request{
		Container:      "web",
		Argv:           []string{"cat"},
		Stdin:          &stdin,
		StripEmptyEnds: boolPointer(false),
	}, fakeDependencies(fake))

	if response.Failed || responseString(response.Stdout) != "done\n" {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if !fake.createOptions.AttachStdin {
		t.Fatal("stdin was not attached")
	}
	fake.connection.mu.Lock()
	defer fake.connection.mu.Unlock()
	if fake.connection.written.String() != stdin+"\n" || !fake.connection.writeClosed {
		t.Fatalf("stdin bytes=%d writeClosed=%t", fake.connection.written.Len(), fake.connection.writeClosed)
	}
}

func TestExecuteDetachedReturnsOnlyExecID(t *testing.T) {
	fake := &execClient{execID: "exec-detached"}
	response := ExecuteWithDependencies(Request{
		Container: "web",
		Argv:      []string{"touch", "/tmp/done"},
		Detach:    true,
	}, fakeDependencies(fake))

	if response.Failed || !response.Changed || response.ExecID != "exec-detached" {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if response.RC != nil || response.Stdout != nil || response.Stderr != nil {
		t.Fatalf("detached response exposes synchronous fields: %#v", response)
	}
	if !fake.startOptions.Detach {
		t.Fatalf("start options = %#v", fake.startOptions)
	}
}

func TestSynchronousResponseRetainsEmptyReturnFields(t *testing.T) {
	fake := &execClient{
		execID:        "exec-empty",
		inspectResult: client.ExecInspectResult{ExitCode: 0},
	}
	response := ExecuteWithDependencies(Request{
		Container: "web",
		Argv:      []string{"true"},
	}, fakeDependencies(fake))
	if response.Failed || response.Stdout == nil || response.Stderr == nil || response.RC == nil {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"stdout":""`, `"stderr":""`, `"rc":0`} {
		if !bytes.Contains(encoded, []byte(field)) {
			t.Errorf("response JSON %s does not contain %s", encoded, field)
		}
	}
	if bytes.Contains(encoded, []byte(`"exec_id"`)) {
		t.Errorf("synchronous response JSON contains exec_id: %s", encoded)
	}
}

func TestExecuteSurfacesEngineSystemErrorStream(t *testing.T) {
	fake := &execClient{
		execID:      "exec-stream-error",
		systemError: "daemon stream failure",
	}
	response := ExecuteWithDependencies(Request{
		Container: "web",
		Argv:      []string{"true"},
	}, fakeDependencies(fake))
	if !response.Failed || !strings.Contains(response.Msg, "daemon stream failure") {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
}

func TestValidateRequestRejectsPinnedUpstreamInvalidCombinations(t *testing.T) {
	empty := ""
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{name: "neither command", req: Request{Container: "web"}, want: "exactly one"},
		{name: "both commands", req: Request{Container: "web", Argv: []string{"true"}, Command: "true"}, want: "exactly one"},
		{name: "detached stdin", req: Request{Container: "web", Argv: []string{"cat"}, Detach: true, Stdin: &empty}, want: "stdin cannot"},
		{name: "non-string env", req: Request{Container: "web", Argv: []string{"env"}, Env: map[string]any{"COUNT": 3}}, want: "non-string value"},
		{name: "old API chdir", req: Request{
			CommonArgs: docker.CommonArgs{APIVersion: stringPointer("1.34")},
			Container:  "web", Argv: []string{"pwd"}, Chdir: "/tmp",
		}, want: "1.35"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := ExecuteWithDependencies(test.req, docker.Dependencies{Environment: docker.StaticEnvironment{}})
			if !response.Failed || !strings.Contains(strings.ToLower(response.Msg), strings.ToLower(test.want)) {
				t.Fatalf("ExecuteWithDependencies() = %#v, want error containing %q", response, test.want)
			}
		})
	}
}

func TestExecuteOmitsStdinAttachmentWhenStdinIsAbsent(t *testing.T) {
	fake := &execClient{execID: "exec-no-stdin", inspectResult: client.ExecInspectResult{ExitCode: 0}}
	response := ExecuteWithDependencies(Request{
		Container: "web",
		Argv:      []string{"cat"},
	}, fakeDependencies(fake))
	if response.Failed || fake.createOptions.AttachStdin {
		t.Fatalf("ExecuteWithDependencies() = %#v attachStdin=%t", response, fake.createOptions.AttachStdin)
	}
}

func TestExecuteStdinNewlineAndStripCombinations(t *testing.T) {
	hello := "Hello world!"
	tests := []struct {
		name           string
		addNewline     *bool
		stripEmptyEnds *bool
		wantWritten    string
		stdout         string
		wantStdout     string
	}{
		{
			name:           "newline preserved",
			stripEmptyEnds: boolPointer(false),
			wantWritten:    hello + "\n",
			stdout:         hello + "\n",
			wantStdout:     hello + "\n",
		},
		{
			name:           "no added newline",
			addNewline:     boolPointer(false),
			stripEmptyEnds: boolPointer(false),
			wantWritten:    hello,
			stdout:         hello,
			wantStdout:     hello,
		},
		{
			name:           "newline then strip",
			addNewline:     boolPointer(true),
			stripEmptyEnds: boolPointer(true),
			wantWritten:    hello + "\n",
			stdout:         hello + "\n",
			wantStdout:     hello,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &execClient{
				execID:        "exec-stdin-combo",
				stdout:        test.stdout,
				inspectResult: client.ExecInspectResult{ExitCode: 0},
			}
			response := ExecuteWithDependencies(Request{
				Container:       "web",
				Argv:            []string{"cat"},
				Stdin:           &hello,
				StdinAddNewline: test.addNewline,
				StripEmptyEnds:  test.stripEmptyEnds,
			}, fakeDependencies(fake))
			if response.Failed || responseInt(response.RC) != 0 || responseString(response.Stdout) != test.wantStdout {
				t.Fatalf("ExecuteWithDependencies() = %#v, want stdout %q", response, test.wantStdout)
			}
			fake.connection.mu.Lock()
			defer fake.connection.mu.Unlock()
			if fake.connection.written.String() != test.wantWritten || !fake.connection.writeClosed {
				t.Fatalf("stdin written=%q closed=%t, want %q", fake.connection.written.String(), fake.connection.writeClosed, test.wantWritten)
			}
		})
	}
}

func TestExecuteMapsPinnedUpstreamEngineErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "missing", err: errdefs.ErrNotFound.WithMessage("missing"), want: `Could not find container "web"`},
		{name: "paused", err: errdefs.ErrConflict.WithMessage("paused"), want: `container "web" has been paused`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &execClient{createErr: test.err}
			response := ExecuteWithDependencies(Request{Container: "web", Argv: []string{"true"}}, fakeDependencies(fake))
			if !response.Failed || !strings.Contains(response.Msg, test.want) {
				t.Fatalf("ExecuteWithDependencies() = %#v", response)
			}
		})
	}
}
