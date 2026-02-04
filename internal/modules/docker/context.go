package docker

import (
	"context"
	"time"
)

// Note: DefaultTimeoutSeconds is defined in client.go

// GetContext creates a context with timeout from CommonArgs.
// If timeout is 0 or negative, uses DefaultTimeoutSeconds.
func GetContext(args CommonArgs) (context.Context, context.CancelFunc) {
	timeout := args.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeoutSeconds
	}
	return context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
}

// GetContextWithTimeout creates a context with the specified timeout in seconds.
// If timeout is 0 or negative, uses DefaultTimeoutSeconds.
func GetContextWithTimeout(timeoutSeconds int) (context.Context, context.CancelFunc) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTimeoutSeconds
	}
	return context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
}

// TimeoutOrDefault returns the timeout from CommonArgs or the default value.
func (args CommonArgs) TimeoutOrDefault() int {
	if args.Timeout <= 0 {
		return DefaultTimeoutSeconds
	}
	return args.Timeout
}
