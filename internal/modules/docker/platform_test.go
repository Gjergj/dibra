package docker

import (
	"reflect"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestParsePlatformMatchesPinnedUpstream(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		value      string
		daemonOS   string
		daemonArch string
		want       ocispec.Platform
	}{
		{name: "architecture only", value: "amd64", daemonOS: "linux", daemonArch: "aarch64",
			want: ocispec.Platform{OS: "linux", Architecture: "amd64"}},
		{name: "operating system only", value: "macos", daemonOS: "linux", daemonArch: "x86_64",
			want: ocispec.Platform{OS: "darwin", Architecture: "amd64"}},
		{name: "architecture alias", value: "linux/x86_64/v1",
			want: ocispec.Platform{OS: "linux", Architecture: "amd64"}},
		{name: "arm64 numeric variant", value: "linux/arm64/8",
			want: ocispec.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}},
		{name: "arm64 canonical variant", value: "linux/aarch64/v8",
			want: ocispec.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}},
		{name: "implicit arm variant", value: "linux/arm",
			want: ocispec.Platform{OS: "linux", Architecture: "arm"}},
		{name: "explicit arm variant", value: "linux/arm/7",
			want: ocispec.Platform{OS: "linux", Architecture: "arm", Variant: "v7"}},
		{name: "unknown explicit OS remains valid", value: "custom/amd64",
			want: ocispec.Platform{OS: "custom", Architecture: "amd64"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParsePlatform(test.value, test.daemonOS, test.daemonArch)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ParsePlatform(%q) = %#v, want %#v", test.value, got, test.want)
			}
		})
	}
}

func TestParsePlatformRejectsPinnedInvalidForms(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value string
		part  string
	}{
		{value: "", part: "non-empty"},
		{value: "unknown", part: "unknown OS or architecture"},
		{value: "/amd64", part: "OS is empty"},
		{value: "linux/", part: "architecture is empty"},
		{value: "linux/amd64/", part: "variant is empty"},
		{value: "linux/amd64/v.1", part: "variant has invalid characters"},
	} {
		if _, err := ParsePlatform(test.value, "linux", "amd64"); err == nil ||
			!strings.Contains(err.Error(), test.part) {
			t.Fatalf("ParsePlatform(%q) error = %v, want substring %q", test.value, err, test.part)
		}
	}
}

func TestComposePlatformNormalizesDaemonFields(t *testing.T) {
	got := ComposePlatform("", "aarch64", "8", "macos", "")
	want := ocispec.Platform{OS: "darwin", Architecture: "arm64"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComposePlatform() = %#v, want %#v", got, want)
	}
}
