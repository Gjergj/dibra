package docker

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestParsePullPushStream(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedDigest string
		expectedError  bool
		expectedLogs   int
	}{
		{
			name: "successful pull",
			input: `{"status":"Pulling from library/alpine"}
{"status":"Digest: sha256:abc123def456"}
{"status":"Status: Downloaded newer image for alpine:latest"}
`,
			expectedDigest: "sha256:abc123def456",
			expectedLogs:   3,
		},
		{
			name: "image up to date",
			input: `{"status":"Pulling from library/alpine"}
{"status":"Digest: sha256:abc123def456"}
{"status":"Status: Image is up to date for alpine:latest"}
`,
			expectedDigest: "sha256:abc123def456",
			expectedLogs:   3,
		},
		{
			name: "pull with layer progress",
			input: `{"status":"Pulling from library/alpine"}
{"status":"Pulling fs layer","id":"abc123"}
{"status":"Downloading","progressDetail":{"current":1024,"total":2048},"id":"abc123"}
{"status":"Pull complete","id":"abc123"}
{"status":"Digest: sha256:abc123def456"}
`,
			expectedDigest: "sha256:abc123def456",
			expectedLogs:   5,
		},
		{
			name: "error in stream",
			input: `{"status":"Pulling from library/nonexistent"}
{"error":"repository does not exist"}
`,
			expectedError: true,
		},
		{
			name: "error with detail",
			input: `{"status":"Pulling from library/private"}
{"errorDetail":{"message":"unauthorized: authentication required"}}
`,
			expectedError: true,
		},
		{
			name:          "error detail without top-level error",
			input:         `{"errorDetail":{"code":401,"message":"denied by registry"}}`,
			expectedError: true,
		},
		{
			name:          "malformed trailing object",
			input:         `{"status":"Pulling"}{"status":`,
			expectedError: true,
		},
		{
			name:  "empty stream",
			input: "",
		},
		{
			name: "aux with digest",
			input: `{"aux":{"Digest":"sha256:newdigest","Tag":"latest"}}
`,
			expectedDigest: "sha256:newdigest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			result := ParsePullPushStream(reader)

			if tt.expectedError {
				if result.Error == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if result.Error != nil {
				t.Fatalf("Unexpected error: %v", result.Error)
			}

			if tt.expectedDigest != "" && result.Digest != tt.expectedDigest {
				t.Errorf("Digest: got %q, want %q", result.Digest, tt.expectedDigest)
			}

			if tt.expectedLogs > 0 && len(result.Logs) != tt.expectedLogs {
				t.Errorf("Logs count: got %d, want %d", len(result.Logs), tt.expectedLogs)
			}
		})
	}
}

func TestParseBuildStream(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedID    string
		expectedError bool
		minLogs       int
	}{
		{
			name: "successful build",
			input: `{"stream":"Step 1/2 : FROM alpine\n"}
{"stream":" ---\u003e abc123\n"}
{"stream":"Step 2/2 : RUN echo hello\n"}
{"stream":" ---\u003e Running in def456\n"}
{"stream":"Removing intermediate container def456\n"}
{"stream":" ---\u003e ghi789\n"}
{"stream":"Successfully built ghi789\n"}
{"aux":{"ID":"sha256:final123"}}
`,
			expectedID: "sha256:final123",
			minLogs:    5,
		},
		{
			name: "build error",
			input: `{"stream":"Step 1/1 : FROM nonexistent:image\n"}
{"error":"pull access denied","errorDetail":{"code":1,"message":"pull access denied"}}
`,
			expectedError: true,
		},
		{
			name: "extract id from stream",
			input: `{"stream":"Successfully built abc123def\n"}
`,
			expectedID: "abc123def",
			minLogs:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			result := ParseBuildStream(reader)

			if tt.expectedError {
				if result.Error == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if result.Error != nil {
				t.Fatalf("Unexpected error: %v", result.Error)
			}

			if tt.expectedID != "" && result.ImageID != tt.expectedID {
				t.Errorf("ImageID: got %q, want %q", result.ImageID, tt.expectedID)
			}

			if tt.minLogs > 0 && len(result.Logs) < tt.minLogs {
				t.Errorf("Expected at least %d logs, got %d", tt.minLogs, len(result.Logs))
			}
		})
	}

	status := ParseBuildStream(strings.NewReader(
		`{"status":"Pulling base layer"}` + "\n" +
			`{"stream":"first\nsecond\n"}`))
	if status.Error != nil || len(status.Logs) != 3 ||
		status.Logs[0] != "Pulling base layer" ||
		status.Logs[1] != "first" || status.Logs[2] != "second" {
		t.Fatalf("mixed build output = %#v", status)
	}
}

func TestParseLoadStream(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedImages []string
		minLogs        int
	}{
		{
			name: "load with tag",
			input: `{"stream":"Loaded image: alpine:latest\n"}
`,
			expectedImages: []string{"alpine:latest"},
			minLogs:        1,
		},
		{
			name: "load with id",
			input: `{"stream":"Loaded image ID: sha256:abc123\n"}
`,
			expectedImages: []string{"sha256:abc123"},
			minLogs:        1,
		},
		{
			name: "multiple images",
			input: `{"stream":"Loaded image: alpine:latest\n"}
{"stream":"Loaded image: nginx:latest\n"}
`,
			expectedImages: []string{"alpine:latest", "nginx:latest"},
			minLogs:        2,
		},
		{
			name:           "plain text response",
			input:          "Loaded image: busybox:latest\n",
			expectedImages: []string{"busybox:latest"},
			minLogs:        1,
		},
		{
			name:           "empty stream",
			input:          "",
			expectedImages: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			result := ParseLoadStream(reader)

			if len(tt.expectedImages) > 0 {
				if len(result.Images) != len(tt.expectedImages) {
					t.Errorf("Images count: got %d, want %d", len(result.Images), len(tt.expectedImages))
				}

				for i, expected := range tt.expectedImages {
					if i < len(result.Images) && result.Images[i] != expected {
						t.Errorf("Image[%d]: got %q, want %q", i, result.Images[i], expected)
					}
				}
			}

			if tt.minLogs > 0 && len(result.Logs) < tt.minLogs {
				t.Errorf("Expected at least %d logs, got %d", tt.minLogs, len(result.Logs))
			}
		})
	}

	preserved := ParseLoadStream(strings.NewReader(
		`{"stream":"  Loaded image: ignored:latest\nLoaded image: kept:latest  \n"}`))
	if len(preserved.Logs) != 2 ||
		preserved.Logs[0] != "  Loaded image: ignored:latest" ||
		preserved.Logs[1] != "Loaded image: kept:latest  " ||
		len(preserved.Images) != 1 || preserved.Images[0] != "kept:latest" {
		t.Fatalf("preserved load output = %#v", preserved)
	}
}

func TestDecodeJSONStreamHandlesArbitraryChunking(t *testing.T) {
	reader := io.MultiReader(
		strings.NewReader(`{"one":`),
		strings.NewReader(`1}{"two":2}`),
	)
	var count int
	err := DecodeJSONStream(reader, func(raw json.RawMessage) error {
		count++
		return nil
	})
	if err != nil || count != 2 {
		t.Fatalf("DecodeJSONStream() count = %d, error = %v", count, err)
	}
}

func TestConsumeStream(t *testing.T) {
	input := "test data to consume"
	reader := strings.NewReader(input)

	err := ConsumeStream(reader)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestStreamToString(t *testing.T) {
	input := "test data\nwith newlines\n"
	reader := strings.NewReader(input)

	result, err := StreamToString(reader)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result != input {
		t.Errorf("StreamToString() = %q, want %q", result, input)
	}
}
