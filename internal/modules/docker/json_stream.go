package docker

import (
	"encoding/json"
	"fmt"
	"io"
)

// DockerStreamError is an error embedded in a successful Docker HTTP response.
// Some Engine versions provide only errorDetail and omit the top-level error.
type DockerStreamError struct {
	Code    int
	Message string
}

func (err *DockerStreamError) Error() string {
	if err.Code != 0 {
		return fmt.Sprintf("Docker stream error (code %d): %s", err.Code, err.Message)
	}
	return err.Message
}

type dockerStreamEnvelope struct {
	Error       string `json:"error"`
	ErrorDetail struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errorDetail"`
}

// DecodeJSONStream decodes concatenated or whitespace-delimited JSON values.
// Docker does not guarantee that transport chunks line up with JSON objects.
func DecodeJSONStream(reader io.Reader, visit func(json.RawMessage) error) error {
	decoder := json.NewDecoder(reader)
	for {
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("invalid Docker JSON stream: %w", err)
		}
		if err := visit(value); err != nil {
			return err
		}
	}
}

func streamEnvelopeError(raw json.RawMessage) error {
	var envelope dockerStreamEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("invalid Docker JSON stream object: %w", err)
	}
	message := envelope.ErrorDetail.Message
	if message == "" {
		message = envelope.Error
	}
	if message == "" {
		return nil
	}
	return &DockerStreamError{Code: envelope.ErrorDetail.Code, Message: message}
}
