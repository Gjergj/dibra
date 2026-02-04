package docker

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// PullPushProgress represents a progress message from docker pull/push.
type PullPushProgress struct {
	Status         string `json:"status"`
	ID             string `json:"id"`
	Progress       string `json:"progress"`
	ProgressDetail struct {
		Current int64 `json:"current"`
		Total   int64 `json:"total"`
	} `json:"progressDetail"`
	Error   string `json:"error"`
	ErrorDetail struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
	Aux struct {
		Tag    string `json:"Tag"`
		Digest string `json:"Digest"`
		Size   int64  `json:"Size"`
	} `json:"aux"`
}

// BuildProgress represents a progress message from docker build.
type BuildProgress struct {
	Stream      string `json:"stream"`
	Error       string `json:"error"`
	ErrorDetail struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errorDetail"`
	Aux struct {
		ID string `json:"ID"`
	} `json:"aux"`
}

// PullResult contains the result of parsing a pull stream.
type PullResult struct {
	ImageID string   // The final image ID (if available)
	Digest  string   // The image digest (if available)
	Logs    []string // Status messages
	Error   error    // Any error encountered
}

// ParsePullPushStream parses the JSON stream from docker pull or push.
// Returns the image ID/digest and any error encountered in the stream.
func ParsePullPushStream(reader io.Reader) PullResult {
	result := PullResult{
		Logs: make([]string, 0),
	}

	decoder := json.NewDecoder(reader)
	for {
		var progress PullPushProgress
		if err := decoder.Decode(&progress); err != nil {
			if err == io.EOF {
				break
			}
			// Try to continue parsing even if one line fails
			continue
		}

		// Check for error in stream
		if progress.Error != "" {
			result.Error = fmt.Errorf("%s", progress.Error)
			return result
		}
		if progress.ErrorDetail.Message != "" {
			result.Error = fmt.Errorf("%s", progress.ErrorDetail.Message)
			return result
		}

		// Capture digest/tag from aux field
		if progress.Aux.Digest != "" {
			result.Digest = progress.Aux.Digest
		}

		// Capture status messages
		if progress.Status != "" {
			if progress.ID != "" {
				result.Logs = append(result.Logs, fmt.Sprintf("%s: %s", progress.ID, progress.Status))
			} else {
				result.Logs = append(result.Logs, progress.Status)
			}

			// Try to extract digest from status
			if strings.HasPrefix(progress.Status, "Digest:") {
				parts := strings.Fields(progress.Status)
				if len(parts) >= 2 {
					result.Digest = parts[1]
				}
			}

			// Try to extract image ID from status
			if strings.HasPrefix(progress.Status, "Status: Downloaded newer image for") ||
				strings.HasPrefix(progress.Status, "Status: Image is up to date for") {
				// Image reference is after "for "
				parts := strings.SplitN(progress.Status, " for ", 2)
				if len(parts) == 2 {
					result.ImageID = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	return result
}

// BuildResult contains the result of parsing a build stream.
type BuildResult struct {
	ImageID string   // The final image ID
	Logs    []string // Build log lines
	Error   error    // Any error encountered
}

// ParseBuildStream parses the JSON stream from docker build.
// Returns the image ID and any error encountered in the stream.
func ParseBuildStream(reader io.Reader) BuildResult {
	result := BuildResult{
		Logs: make([]string, 0),
	}

	decoder := json.NewDecoder(reader)
	for {
		var progress BuildProgress
		if err := decoder.Decode(&progress); err != nil {
			if err == io.EOF {
				break
			}
			// Try to continue parsing even if one line fails
			continue
		}

		// Check for error in stream
		if progress.Error != "" {
			result.Error = fmt.Errorf("%s", progress.Error)
			return result
		}
		if progress.ErrorDetail.Message != "" {
			result.Error = fmt.Errorf("build failed with code %d: %s",
				progress.ErrorDetail.Code, progress.ErrorDetail.Message)
			return result
		}

		// Capture image ID from aux field
		if progress.Aux.ID != "" {
			result.ImageID = progress.Aux.ID
		}

		// Capture stream output (build logs)
		if progress.Stream != "" {
			// Remove trailing newline for cleaner logs
			line := strings.TrimRight(progress.Stream, "\n\r")
			if line != "" {
				result.Logs = append(result.Logs, line)
			}

			// Try to extract image ID from stream
			// Format: "Successfully built <id>"
			if strings.HasPrefix(line, "Successfully built ") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					result.ImageID = parts[2]
				}
			}
		}
	}

	return result
}

// LoadResult contains the result of parsing an image load stream.
type LoadResult struct {
	Images []string // Loaded image references
	Logs   []string // Status messages
	Error  error    // Any error encountered
}

// ParseLoadStream parses the output from docker image load.
// The load API returns a stream with {"stream": "..."} messages.
func ParseLoadStream(reader io.Reader) LoadResult {
	result := LoadResult{
		Images: make([]string, 0),
		Logs:   make([]string, 0),
	}

	// Try JSON decoding first
	decoder := json.NewDecoder(reader)
	jsonParsed := false

	for {
		var msg struct {
			Stream string `json:"stream"`
		}
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			// If JSON parsing fails, fall back to line-by-line
			break
		}
		jsonParsed = true

		if msg.Stream != "" {
			line := strings.TrimSpace(msg.Stream)
			if line != "" {
				result.Logs = append(result.Logs, line)
				extractLoadedImage(&result, line)
			}
		}
	}

	// If JSON parsing didn't work, try plain text
	if !jsonParsed {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				result.Logs = append(result.Logs, line)
				extractLoadedImage(&result, line)
			}
		}
	}

	return result
}

// extractLoadedImage extracts image names from load output lines.
func extractLoadedImage(result *LoadResult, line string) {
	// Format: "Loaded image: <name:tag>" or "Loaded image ID: sha256:<id>"
	if strings.HasPrefix(line, "Loaded image:") {
		image := strings.TrimSpace(strings.TrimPrefix(line, "Loaded image:"))
		if image != "" {
			result.Images = append(result.Images, image)
		}
	} else if strings.HasPrefix(line, "Loaded image ID:") {
		image := strings.TrimSpace(strings.TrimPrefix(line, "Loaded image ID:"))
		if image != "" {
			result.Images = append(result.Images, image)
		}
	}
}

// ConsumeStream reads and discards the stream content.
// Useful when you need to complete an operation but don't need the output.
func ConsumeStream(reader io.Reader) error {
	_, err := io.Copy(io.Discard, reader)
	return err
}

// StreamToString reads the entire stream into a string.
func StreamToString(reader io.Reader) (string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
