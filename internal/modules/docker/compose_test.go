package docker

import (
	"reflect"
	"testing"
)

func TestParseComposeJSONDocuments(t *testing.T) {
	documents, err := ParseComposeJSONDocuments([]byte("{\"Name\":\"web\"}\n{\"Name\":\"db\"}\n"))
	if err != nil || len(documents) != 2 {
		t.Fatalf("ndjson = %#v (%v)", documents, err)
	}
	array, err := ParseComposeJSONDocuments([]byte(`[{"Name":"web"}]`))
	if err != nil || len(array) != 1 {
		t.Fatalf("array = %#v (%v)", array, err)
	}
	images, err := ParseComposeJSONDocuments([]byte(`{"web":{"Repository":"alpine","Tag":"latest"}}`))
	if err != nil || len(images) != 1 {
		t.Fatalf("image map = %#v (%v)", images, err)
	}
}

func TestNormalizeComposeContainer(t *testing.T) {
	container, err := NormalizeComposeContainer(map[string]any{
		"Name": "demo-web-1", "Labels": "com.docker.compose.service=web,foo=bar", "Networks": "frontend,backend",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(container["Names"], []any{"demo-web-1"}) {
		t.Fatalf("names = %#v", container["Names"])
	}
	labels := container["Labels"].(map[string]any)
	if labels["com.docker.compose.service"] != "web" || labels["foo"] != "bar" {
		t.Fatalf("labels = %#v", labels)
	}
}

func TestGetComposeProjectArgsIncludesJSONProgress(t *testing.T) {
	args, err := GetComposeProjectArgsWithEnvironment(ComposeCommonArgs{
		ProjectSrc: "/project", ProjectName: "demo", Files: []string{"compose.yaml"}, EnvFiles: []string{".env"}, Profiles: []string{"web"},
	}, CommonArgs{}, StaticEnvironment{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--host", DefaultDockerHost, "compose", "--ansi", "never", "--progress", "json",
		"--project-directory", "/project", "--project-name", "demo",
		"--file", "compose.yaml", "--env-file", ".env", "--profile", "web",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %#v, want %#v", args, want)
	}
}

func TestGetComposeProjectArgsSupportsPlainProgress(t *testing.T) {
	args, err := GetComposeProjectArgsWithProgressEnvironment(ComposeCommonArgs{
		ProjectSrc: "/project", ProjectName: "demo",
	}, CommonArgs{}, StaticEnvironment{}, "plain")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--host", DefaultDockerHost, "compose", "--ansi", "never", "--progress", "plain",
		"--project-directory", "/project", "--project-name", "demo",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %#v, want %#v", args, want)
	}
}
