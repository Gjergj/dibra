package docker

import (
	"context"
	"time"
)

// Note: DefaultTimeoutSeconds is defined in client.go

// GetContext creates a context with the resolved timeout from CommonArgs.
func GetContext(args CommonArgs) (context.Context, context.CancelFunc) {
	return GetContextWithEnvironment(args, OSEnvironment{})
}

// GetContextWithEnvironment applies the same injectable timeout resolution as
// the Engine API client.
func GetContextWithEnvironment(args CommonArgs, environment Environment) (context.Context, context.CancelFunc) {
	timeout := args.TimeoutOrDefault()
	if connection, err := ResolveConnectionWithEnvironment(args, environment); err == nil {
		timeout = connection.Timeout
	}
	if timeout <= 0 {
		return context.WithCancel(context.Background())
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
	if args.Timeout == nil {
		return DefaultTimeoutSeconds
	}
	return *args.Timeout
}
