package docker

import (
	"errors"
	"fmt"
	"strings"

	"github.com/containerd/errdefs"
)

// DockerError wraps a Docker API error with additional context.
type DockerError struct {
	Operation string
	Target    string
	Err       error
}

func (e *DockerError) Error() string {
	if e.Target != "" {
		return fmt.Sprintf("%s %s: %v", e.Operation, e.Target, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Operation, e.Err)
}

func (e *DockerError) Unwrap() error {
	return e.Err
}

// WrapError wraps an error with operation and target context.
func WrapError(operation, target string, err error) error {
	if err == nil {
		return nil
	}
	return &DockerError{
		Operation: operation,
		Target:    target,
		Err:       err,
	}
}

// IsNotFoundError checks if the error indicates a resource was not found.
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check errdefs
	if errdefs.IsNotFound(err) {
		return true
	}
	// Check wrapped error
	var dockerErr *DockerError
	if errors.As(err, &dockerErr) {
		return IsNotFoundError(dockerErr.Err)
	}
	return false
}

// IsConflictError checks if the error indicates a conflict (e.g., resource in use).
func IsConflictError(err error) bool {
	if err == nil {
		return false
	}
	// Check errdefs
	if errdefs.IsConflict(err) {
		return true
	}
	// Check error message
	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "conflict") ||
		strings.Contains(errStr, "in use") ||
		strings.Contains(errStr, "already exists") {
		return true
	}
	// Check wrapped error
	var dockerErr *DockerError
	if errors.As(err, &dockerErr) {
		return IsConflictError(dockerErr.Err)
	}
	return false
}

// IsUnauthorizedError checks if the error indicates authentication failure.
func IsUnauthorizedError(err error) bool {
	if err == nil {
		return false
	}
	// Check errdefs
	if errdefs.IsUnauthorized(err) {
		return true
	}
	// Check error message
	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "authentication required") ||
		strings.Contains(errStr, "access denied") {
		return true
	}
	// Check wrapped error
	var dockerErr *DockerError
	if errors.As(err, &dockerErr) {
		return IsUnauthorizedError(dockerErr.Err)
	}
	return false
}

// IsNetworkError checks if the error is a network-related error.
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "timeout")
}

// IsAlreadyExistsError checks if the error indicates a resource already exists.
func IsAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "already exists") ||
		strings.Contains(errStr, "name is already in use")
}

// IsForbiddenError checks if the error indicates a forbidden operation.
func IsForbiddenError(err error) bool {
	if err == nil {
		return false
	}
	if errdefs.IsPermissionDenied(err) {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "forbidden") ||
		strings.Contains(errStr, "permission denied")
}

// IsContainerRunningError checks if error is because container is running.
func IsContainerRunningError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "container is running") ||
		strings.Contains(errStr, "stop the container before")
}

// ErrorResponse represents a standardized error response.
type ErrorResponse struct {
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

// NewErrorResponse creates a new error response from an error.
func NewErrorResponse(operation, target string, err error) ErrorResponse {
	resp := ErrorResponse{
		Failed: true,
		Msg:    WrapError(operation, target, err).Error(),
	}

	// Categorize error
	switch {
	case IsNotFoundError(err):
		resp.Code = "not_found"
	case IsConflictError(err):
		resp.Code = "conflict"
	case IsUnauthorizedError(err):
		resp.Code = "unauthorized"
	case IsForbiddenError(err):
		resp.Code = "forbidden"
	case IsNetworkError(err):
		resp.Code = "network_error"
	case IsContainerRunningError(err):
		resp.Code = "container_running"
	default:
		resp.Code = "error"
	}

	return resp
}
