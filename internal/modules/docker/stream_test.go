package docker

import (
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
