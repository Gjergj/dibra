package docker_image_build

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type testFileInfo struct {
	name string
	mode fs.FileMode
}

func (info testFileInfo) Name() string       { return info.name }
func (info testFileInfo) Size() int64        { return 0 }
func (info testFileInfo) Mode() fs.FileMode  { return info.mode }
func (info testFileInfo) ModTime() time.Time { return time.Time{} }
func (info testFileInfo) IsDir() bool        { return info.mode.IsDir() }
func (info testFileInfo) Sys() any           { return nil }

type buildFileSystem struct {
	docker.FileSystem
	files map[string]fs.FileMode
}

func (fileSystem buildFileSystem) Stat(path string) (fs.FileInfo, error) {
	mode, found := fileSystem.files[path]
	if !found {
		return nil, fs.ErrNotExist
	}
	return testFileInfo{name: path, mode: mode}, nil
}

type cliReply struct {
	result docker.CLIResult
	err    error
}

type buildRunner struct {
	commands []docker.CLICommand
	replies  []cliReply
}

func (runner *buildRunner) Run(_ context.Context, command docker.CLICommand) (docker.CLIResult, error) {
	runner.commands = append(runner.commands, command)
	if len(runner.replies) == 0 {
		return docker.CLIResult{}, errors.New("unexpected command")
	}
	reply := runner.replies[0]
	runner.replies = runner.replies[1:]
	return reply.result, reply.err
}

func buildDependencies(runner *buildRunner, files map[string]fs.FileMode) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		FileSystem:  buildFileSystem{files: files},
		CLIRunner:   runner,
	}
}

func TestBuildUsesPinnedUpstreamCommandContractAndRawInspection(t *testing.T) {
	runner := &buildRunner{replies: []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte("Error: No such image")}, err: errors.New("exit 1")},
		{result: docker.CLIResult{Stdout: []byte("stdout"), Stderr: []byte("build progress")}},
		{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:new","Config":{"Labels":{"release":"1"}}}`)}},
	}}
	response := ExecuteWithDependencies(Request{
		Name:       "example:v1",
		Tag:        "ignored",
		Path:       "/context",
		Dockerfile: "Dockerfile.custom",
		CacheFrom:  StringList{"cache:v1"},
		Pull:       true,
		Network:    "host",
		NoCache:    true,
		EtcHosts:   map[string]any{"host.test": "host-gateway"},
		Args:       map[string]any{"COUNT": 3, "RELEASE": "1"},
		Target:     "runtime",
		Platform:   StringList{"linux/amd64"},
		ShmSize:    "128MB",
		Labels:     map[string]any{"release": 1},
	}, buildDependencies(runner, map[string]fs.FileMode{
		"/context":                   fs.ModeDir,
		"/context/Dockerfile.custom": 0,
	}))

	if response.Failed || !response.Changed || response.Image["Id"] != "sha256:new" {
		t.Fatalf("response = %#v", response)
	}
	if response.Stdout != "stdout" || response.Stderr != "build progress" {
		t.Fatalf("stdout/stderr = %q/%q", response.Stdout, response.Stderr)
	}
	command := response.Command
	for _, expected := range [][]string{
		{"--tag", "example:v1"},
		{"--file", "/context/Dockerfile.custom"},
		{"--cache-from", "cache:v1"},
		{"--pull"},
		{"--network", "host"},
		{"--no-cache"},
		{"--add-host", "host.test:host-gateway"},
		{"--build-arg", "COUNT=3"},
		{"--target", "runtime"},
		{"--platform", "linux/amd64"},
		{"--shm-size", "134217728"},
		{"--label", "release=1"},
		{"--", "/context"},
	} {
		if !containsSequence(command, expected) {
			t.Errorf("command %q does not contain %q", command, expected)
		}
	}
}

func TestOutputsAddNamedImageAndValueSecretWithoutExposingValue(t *testing.T) {
	runner := &buildRunner{replies: []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:old"}`)}},
		{result: docker.CLIResult{}},
		{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:new"}`)}},
	}}
	response := ExecuteWithDependencies(Request{
		Name:    "example",
		Path:    "/context",
		Rebuild: "always",
		Secrets: []Secret{{ID: "token", Type: "value", Value: "very-secret"}},
		Outputs: []Output{{Type: "tar", Dest: "/tmp/rootfs.tar"}},
	}, buildDependencies(runner, map[string]fs.FileMode{"/context": fs.ModeDir}))

	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if containsSequence(response.Command, []string{"--tag"}) {
		t.Fatalf("command unexpectedly contains --tag: %q", response.Command)
	}
	if !containsSequence(response.Command, []string{"--output", "type=tar,dest=/tmp/rootfs.tar"}) ||
		!containsSequence(response.Command, []string{"--output", "type=image,name=example:latest"}) {
		t.Fatalf("outputs missing from command: %q", response.Command)
	}
	if strings.Contains(strings.Join(response.Command, " "), "very-secret") {
		t.Fatalf("secret value leaked in command: %q", response.Command)
	}
	buildInvocation := runner.commands[2]
	if !slices.ContainsFunc(buildInvocation.Env, func(value string) bool {
		return strings.HasPrefix(value, "ANSIBLE_DOCKER_COMPOSE_ENV_SECRET_") && strings.HasSuffix(value, "=very-secret")
	}) {
		t.Fatalf("secret environment missing: %#v", buildInvocation.Env)
	}
}

func TestCheckModeValidatesButDoesNotBuild(t *testing.T) {
	runner := &buildRunner{replies: []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte("No such image")}, err: errors.New("exit 1")},
	}}
	response := ExecuteWithDependenciesAndState(Request{Name: "example", Path: "/context"},
		buildDependencies(runner, map[string]fs.FileMode{"/context": fs.ModeDir}),
		execution.State{CheckMode: true})

	if response.Failed || !response.Changed || len(runner.commands) != 2 {
		t.Fatalf("response = %#v; commands = %#v", response, runner.commands)
	}
}

func TestBuildxVersionAndNestedValidation(t *testing.T) {
	tests := []struct {
		name    string
		request Request
		version string
		want    string
	}{
		{
			name:    "multiple outputs",
			request: Request{Name: "example", Path: "/context", Outputs: []Output{{Type: "tar", Dest: "/one"}, {Type: "oci", Dest: "/two"}}},
			version: "v0.12.0",
			want:    "0.13.0 is needed",
		},
		{
			name:    "environment secret",
			request: Request{Name: "example", Path: "/context", Secrets: []Secret{{ID: "secret", Type: "env", Env: "TOKEN"}}},
			version: "v0.5.0",
			want:    "0.6.0 is needed",
		},
		{
			name:    "output destination",
			request: Request{Name: "example", Path: "/context", Outputs: []Output{{Type: "tar"}}},
			version: "v0.13.1",
			want:    "dest is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &buildRunner{replies: []cliReply{{result: docker.CLIResult{Output: []byte(test.version)}}}}
			response := ExecuteWithDependencies(test.request,
				buildDependencies(runner, map[string]fs.FileMode{"/context": fs.ModeDir}))
			if !response.Failed || !strings.Contains(response.Msg, test.want) {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestQuoteCSVMatchesUpstreamFieldQuoting(t *testing.T) {
	tests := []struct {
		value    string
		expected string
	}{
		{value: "", expected: ""},
		{value: " ", expected: `" "`},
		{value: ",", expected: `","`},
		{value: `"`, expected: `""""`},
		{value: "\rhello, \"hi\" !\n", expected: "\"\rhello, \"\"hi\"\" !\n\""},
	}
	for _, test := range tests {
		if got := quoteCSV([]string{test.value}); got != test.expected {
			t.Errorf("quoteCSV(%q) = %q, want %q", test.value, got, test.expected)
		}
	}
}

func TestFileSecretAddsSrcWithoutLeakingValue(t *testing.T) {
	runner := &buildRunner{replies: []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte("Error: No such image")}, err: errors.New("exit 1")},
		{result: docker.CLIResult{Stdout: []byte("stdout"), Stderr: []byte("build")}},
		{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:new"}`)}},
	}}
	response := ExecuteWithDependencies(Request{
		Name:    "example:v1",
		Path:    "/context",
		Secrets: []Secret{{ID: "npm", Type: "file", Src: "/context/token"}},
	}, buildDependencies(runner, map[string]fs.FileMode{
		"/context":       fs.ModeDir,
		"/context/token": 0,
	}))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if !containsSequence(response.Command, []string{"--secret", "id=npm,type=file,src=/context/token"}) {
		t.Fatalf("file secret missing from command: %q", response.Command)
	}
}

func TestStringListAcceptsScalarAndList(t *testing.T) {
	for input, count := range map[string]int{`"linux/amd64"`: 1, `["linux/amd64","linux/arm64"]`: 2} {
		var values StringList
		if err := values.UnmarshalJSON([]byte(input)); err != nil || len(values) != count {
			t.Fatalf("UnmarshalJSON(%s) = %#v, %v", input, values, err)
		}
	}
}

func TestDockerOutputOmitsEmptyDestAndContext(t *testing.T) {
	// community.docker#946: `--output type=docker,` is an invalid value.
	response := successfulBuild(t, Request{
		Name:    "example",
		Tag:     "v1",
		Path:    "/context",
		Outputs: []Output{{Type: "docker"}},
	}, nil)
	if containsSequence(response.Command, []string{"--tag"}) {
		t.Fatalf("outputs must suppress --tag: %q", response.Command)
	}
	if !containsSequence(response.Command, []string{"--output", "type=docker"}) ||
		!containsSequence(response.Command, []string{"--output", "type=image,name=example:v1"}) {
		t.Fatalf("docker/image outputs missing: %q", response.Command)
	}
	for _, part := range response.Command {
		if strings.HasPrefix(part, "type=docker,") {
			t.Fatalf("trailing comma after type=docker: %q", response.Command)
		}
	}
}

func TestImageOutputNameListDoesNotAddTag(t *testing.T) {
	// community.docker#1001 / PR #1006: name may be a list; --tag overwrites output names.
	response := successfulBuild(t, Request{
		Name: "example",
		Path: "/context",
		Outputs: []Output{{
			Type: "image",
			Name: StringList{"example:latest", "example:extra"},
		}},
	}, nil)
	if containsSequence(response.Command, []string{"--tag"}) {
		t.Fatalf("image outputs must not pass --tag: %q", response.Command)
	}
	if !containsSequence(response.Command, []string{"--output", `type=image,"name=example:latest,example:extra"`}) {
		t.Fatalf("name list missing from output: %q", response.Command)
	}
	if outputCount(response.Command, "--output") != 1 {
		t.Fatalf("unexpected extra --output: %q", response.Command)
	}
}

func TestEnvSecretUsesDocumentedEnvField(t *testing.T) {
	response := successfulBuild(t, Request{
		Name:    "example",
		Path:    "/context",
		Secrets: []Secret{{ID: "token", Type: "env", Env: "BUILD_TOKEN"}},
	}, nil)
	if !containsSequence(response.Command, []string{"--secret", "id=token,type=env,env=BUILD_TOKEN"}) {
		t.Fatalf("env secret missing: %q", response.Command)
	}
}

func TestImageOutputPushAndLocalOCIFlags(t *testing.T) {
	response := successfulBuild(t, Request{
		Name: "example",
		Path: "/context",
		Outputs: []Output{
			{Type: "local", Dest: "/tmp/rootfs"},
			{Type: "oci", Dest: "/tmp/image.oci"},
			{Type: "image", Name: StringList{"example:latest"}, Push: true},
		}},
		nil)
	for _, expected := range [][]string{
		{"--output", "type=local,dest=/tmp/rootfs"},
		{"--output", "type=oci,dest=/tmp/image.oci"},
		{"--output", "type=image,name=example:latest,push=true"},
	} {
		if !containsSequence(response.Command, expected) {
			t.Errorf("command %q does not contain %q", response.Command, expected)
		}
	}
	if containsSequence(response.Command, []string{"--tag"}) {
		t.Fatalf("outputs must suppress --tag: %q", response.Command)
	}
}

func TestEmbeddedNameTagWinsOverTagOption(t *testing.T) {
	runner := &buildRunner{replies: missingImageReplies("stdout", "stderr")}
	response := ExecuteWithDependencies(Request{
		Name: "example:v2",
		Tag:  "ignored",
		Path: "/context",
	}, buildDependencies(runner, map[string]fs.FileMode{"/context": fs.ModeDir}))
	if response.Failed || !containsSequence(response.Command, []string{"--tag", "example:v2"}) {
		t.Fatalf("response = %#v command = %q", response, response.Command)
	}
	if !containsSequence(runner.commands[1].Args, []string{"image", "inspect", "example:v2"}) {
		t.Fatalf("inspect used wrong name: %#v", runner.commands[1].Args)
	}
}

func TestCustomDockerCLIAndRelativeDockerfile(t *testing.T) {
	runner := &buildRunner{replies: missingImageReplies("stdout", "stderr")}
	response := ExecuteWithDependencies(Request{
		Name:       "example",
		Path:       "/context",
		Dockerfile: "nested/Dockerfile",
		DockerCLI:  "/opt/bin/docker",
		Platform:   StringList{"linux/amd64", "linux/arm64"},
	}, buildDependencies(runner, map[string]fs.FileMode{
		"/context":                   fs.ModeDir,
		"/context/nested/Dockerfile": 0,
	}))
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	if runner.commands[0].Name != "/opt/bin/docker" {
		t.Fatalf("docker_cli not used: %#v", runner.commands[0])
	}
	if !containsSequence(response.Command, []string{"--file", "/context/nested/Dockerfile"}) {
		t.Fatalf("relative dockerfile missing: %q", response.Command)
	}
	if !containsSequence(response.Command, []string{"--platform", "linux/amd64"}) ||
		!containsSequence(response.Command, []string{"--platform", "linux/arm64"}) {
		t.Fatalf("platform list missing: %q", response.Command)
	}
}

func TestExistingImageIsUnchangedUnlessRebuildAlways(t *testing.T) {
	existing := []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:old"}`)}},
	}
	unchanged := ExecuteWithDependencies(Request{Name: "example", Path: "/context"},
		buildDependencies(&buildRunner{replies: existing}, map[string]fs.FileMode{"/context": fs.ModeDir}))
	if unchanged.Failed || unchanged.Changed || unchanged.Image["Id"] != "sha256:old" || len(unchanged.Command) != 0 {
		t.Fatalf("rebuild=never = %#v", unchanged)
	}

	always := ExecuteWithDependencies(Request{Name: "example", Path: "/context", Rebuild: "always"},
		buildDependencies(&buildRunner{replies: []cliReply{
			{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
			{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:old"}`)}},
			{result: docker.CLIResult{Stdout: []byte("stdout")}},
			{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:new"}`)}},
		}}, map[string]fs.FileMode{"/context": fs.ModeDir}))
	if always.Failed || !always.Changed || always.Image["Id"] != "sha256:new" {
		t.Fatalf("rebuild=always = %#v", always)
	}
}

func TestCheckModeLeavesExistingImageUnchanged(t *testing.T) {
	runner := &buildRunner{replies: []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:old"}`)}},
	}}
	response := ExecuteWithDependenciesAndState(Request{Name: "example", Path: "/context"},
		buildDependencies(runner, map[string]fs.FileMode{"/context": fs.ModeDir}),
		execution.State{CheckMode: true})
	if response.Failed || response.Changed || len(runner.commands) != 2 || len(response.Command) != 0 {
		t.Fatalf("response = %#v; commands = %#v", response, runner.commands)
	}
}

func TestFailedBuildReturnsCommandStdoutAndStderr(t *testing.T) {
	runner := &buildRunner{replies: []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte("Error: No such image")}, err: errors.New("exit 1")},
		{result: docker.CLIResult{Stdout: []byte("out"), Stderr: []byte("ERROR: fail")}, err: errors.New("exit 1")},
	}}
	response := ExecuteWithDependencies(Request{Name: "example", Path: "/context"},
		buildDependencies(runner, map[string]fs.FileMode{"/context": fs.ModeDir}))
	if !response.Failed || !response.Changed || response.Msg != "Building example:latest failed" {
		t.Fatalf("response = %#v", response)
	}
	if !containsSequence(response.Command, []string{"buildx", "build"}) {
		t.Fatalf("failed build omitted command: %q", response.Command)
	}
	if response.Stdout != "out" || response.Stderr != "ERROR: fail" {
		t.Fatalf("stdout/stderr = %q/%q", response.Stdout, response.Stderr)
	}
}

func TestMissingImageInspectVariantsAreAbsent(t *testing.T) {
	for _, message := range []string{"Error: No such image: example:latest", "Error: No such object: example:latest"} {
		t.Run(message, func(t *testing.T) {
			response := ExecuteWithDependenciesAndState(Request{Name: "example", Path: "/context"},
				buildDependencies(&buildRunner{replies: []cliReply{
					{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
					{result: docker.CLIResult{Output: []byte(message)}, err: errors.New("exit 1")},
				}}, map[string]fs.FileMode{"/context": fs.ModeDir}),
				execution.State{CheckMode: true})
			if response.Failed || !response.Changed || len(response.Image) != 0 {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestRequestValidationFailures(t *testing.T) {
	files := map[string]fs.FileMode{"/context": fs.ModeDir, "/context/token": 0}
	tests := []struct {
		name    string
		request Request
		want    string
	}{
		{name: "missing name", request: Request{Path: "/context"}, want: "name is required"},
		{name: "missing path", request: Request{Name: "example"}, want: "path is required"},
		{name: "invalid rebuild", request: Request{Name: "example", Path: "/context", Rebuild: "sometimes"}, want: "rebuild must be one of never or always"},
		{name: "image id", request: Request{Name: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Path: "/context"}, want: "Image name must not be a digest"},
		{name: "digest name", request: Request{Name: "alpine@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Path: "/context"}, want: "Image name must not be a digest"},
		{name: "invalid tag", request: Request{Name: "example", Tag: "-bad", Path: "/context"}, want: "is not a valid docker tag"},
		{name: "secret missing id", request: Request{Name: "example", Path: "/context", Secrets: []Secret{{Type: "file", Src: "/context/token"}}}, want: "id is required"},
		{name: "file secret missing src", request: Request{Name: "example", Path: "/context", Secrets: []Secret{{ID: "token", Type: "file"}}}, want: "src is required"},
		{name: "env secret missing env", request: Request{Name: "example", Path: "/context", Secrets: []Secret{{ID: "token", Type: "env"}}}, want: "env is required"},
		{name: "value secret missing value", request: Request{Name: "example", Path: "/context", Secrets: []Secret{{ID: "token", Type: "value"}}}, want: "value is required"},
		{name: "secret fields exclusive", request: Request{Name: "example", Path: "/context", Secrets: []Secret{{ID: "token", Type: "file", Src: "/context/token", Env: "TOKEN"}}}, want: "mutually exclusive"},
		{name: "unknown secret type", request: Request{Name: "example", Path: "/context", Secrets: []Secret{{ID: "token", Type: "s3"}}}, want: "must be one of file, env, or value"},
		{name: "tar dest required", request: Request{Name: "example", Path: "/context", Outputs: []Output{{Type: "tar"}}}, want: "dest is required"},
		{name: "local dest required", request: Request{Name: "example", Path: "/context", Outputs: []Output{{Type: "local"}}}, want: "dest is required"},
		{name: "oci dest required", request: Request{Name: "example", Path: "/context", Outputs: []Output{{Type: "oci"}}}, want: "dest is required"},
		{name: "dest exclusive with name", request: Request{Name: "example", Path: "/context", Outputs: []Output{{Type: "tar", Dest: "/tmp/a.tar", Name: StringList{"example:latest"}}}}, want: "dest is mutually exclusive with name and push"},
		{name: "unknown output type", request: Request{Name: "example", Path: "/context", Outputs: []Output{{Type: "registry"}}}, want: "must be one of local, tar, oci, docker, or image"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := ExecuteWithDependencies(test.request, buildDependencies(&buildRunner{}, files))
			if !response.Failed || !strings.Contains(response.Msg, test.want) {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestByteValueMatchesDocumentedUnits(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{input: "128MB", want: 134217728},
		{input: "1G", want: 1 << 30},
		{input: "64", want: 64},
		{input: "2K", want: 2048},
	}
	for _, test := range tests {
		got, err := byteValue(test.input)
		if err != nil || got != test.want {
			t.Errorf("byteValue(%q) = %d, %v, want %d", test.input, got, err, test.want)
		}
	}
	if _, err := byteValue("128XB"); err == nil {
		t.Fatal("expected invalid unit")
	}
}

func TestQuoteCSVJoinsAndQuotesFields(t *testing.T) {
	if got := quoteCSV([]string{"type=image", "name=example:latest"}); got != "type=image,name=example:latest" {
		t.Fatalf("plain fields = %q", got)
	}
	if got := quoteCSV([]string{"type=tar", "dest=/tmp/a,b"}); got != `type=tar,"dest=/tmp/a,b"` {
		t.Fatalf("comma dest = %q", got)
	}
}

func TestParseBuildxVersion(t *testing.T) {
	got, err := parseBuildxVersion("github.com/docker/buildx v0.30.0 123abcd")
	if err != nil || got != [3]int{0, 30, 0} {
		t.Fatalf("got %#v, %v", got, err)
	}
	if _, err := parseBuildxVersion("buildx without a version"); err == nil {
		t.Fatal("expected parse failure")
	}
}

func successfulBuild(t *testing.T, request Request, files map[string]fs.FileMode) Response {
	t.Helper()
	if files == nil {
		files = map[string]fs.FileMode{"/context": fs.ModeDir}
	}
	response := ExecuteWithDependencies(request, buildDependencies(&buildRunner{replies: missingImageReplies("stdout", "stderr")}, files))
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	return response
}

func missingImageReplies(stdout, stderr string) []cliReply {
	return []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte("Error: No such image")}, err: errors.New("exit 1")},
		{result: docker.CLIResult{Stdout: []byte(stdout), Stderr: []byte(stderr)}},
		{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:new"}`)}},
	}
}

func outputCount(command []string, option string) int {
	count := 0
	for _, value := range command {
		if value == option {
			count++
		}
	}
	return count
}

func containsSequence(values, wanted []string) bool {
	if len(wanted) == 0 {
		return true
	}
	for start := 0; start+len(wanted) <= len(values); start++ {
		if slices.Equal(values[start:start+len(wanted)], wanted) {
			return true
		}
	}
	return false
}
