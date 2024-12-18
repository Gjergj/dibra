package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/smithy-go/ptr"
)

// PackageState represents the desired state of a package
type PackageState string

const (
	Present  PackageState = "present"
	Absent   PackageState = "absent"
	Latest   PackageState = "latest"
	BuildDep PackageState = "build-dep"
	Fixed    PackageState = "fixed"
)

// PackageManager handles apt operations
type PackageManager struct {
	dpkgOptions    []string
	defaultTimeout time.Duration
	cacheValidTime time.Duration
	forceAptGet    bool
	lockTimeout    time.Duration
	updateRetries  int
	maxRetryDelay  time.Duration
}

// PackageOptions configures package operations
type PackageOptions struct {
	State                   PackageState
	UpdateCache             bool
	Cache                   bool
	ForceApt                bool
	InstallRecommends       *bool
	Force                   bool
	AutoRemove              bool
	Purge                   bool
	AllowUnauthenticated    bool
	AllowDowngrade          bool
	AllowChangeHeldPackages bool
	OnlyUpgrade             bool
	FailOnAutoremove        bool
	DefaultRelease          string
	PolicyRcD               *int
}

// NewPackageManager creates a new apt package manager
func NewPackageManager(opts ...Option) *PackageManager {
	pm := &PackageManager{
		dpkgOptions:    []string{"force-confdef", "force-confold"},
		defaultTimeout: 5 * time.Minute,
		cacheValidTime: time.Hour,
		lockTimeout:    60 * time.Second,
		updateRetries:  5,
		maxRetryDelay:  12 * time.Second,
	}

	for _, opt := range opts {
		opt(pm)
	}

	return pm
}

// Install installs or updates packages
func (pm *PackageManager) Install(ctx context.Context, packages []string, opts PackageOptions) error {
	if len(packages) == 0 {
		return nil
	}

	// Update cache if requested
	if opts.UpdateCache {
		if err := pm.UpdateCache(ctx); err != nil {
			return fmt.Errorf("failed to update cache: %w", err)
		}
	}

	args := []string{
		"-y",
		"-q",
	}

	// Add options
	if opts.Force {
		args = append(args, "--force-yes")
	}

	if opts.AllowUnauthenticated {
		args = append(args, "--allow-unauthenticated")
	}

	if opts.AllowDowngrade {
		args = append(args, "--allow-downgrades")
	}

	if opts.OnlyUpgrade {
		args = append(args, "--only-upgrade")
	}

	// Add dpkg options
	for _, opt := range pm.dpkgOptions {
		args = append(args, "-o", fmt.Sprintf("Dpkg::Options::=--%s", opt))
	}

	// Add install command and packages
	args = append(args, "install")
	args = append(args, packages...)

	// Execute apt-get command
	cmd := exec.CommandContext(ctx, "apt-get", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt-get install failed: %w\nOutput: %s", err, output)
	}

	return nil
}

// Remove removes packages
func (pm *PackageManager) Remove(ctx context.Context, packages []string, opts PackageOptions) error {
	if len(packages) == 0 {
		return nil
	}

	args := []string{
		"-y",
		"-q",
	}

	if opts.Purge {
		args = append(args, "--purge")
	}

	if opts.AutoRemove {
		args = append(args, "--auto-remove")
	}

	// Add dpkg options
	for _, opt := range pm.dpkgOptions {
		args = append(args, "-o", fmt.Sprintf("Dpkg::Options::=--%s", opt))
	}

	args = append(args, "remove")
	args = append(args, packages...)

	cmd := exec.CommandContext(ctx, "apt-get", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt-get remove failed: %w\nOutput: %s", err, output)
	}

	return nil
}

// UpdateCache updates the apt cache
func (pm *PackageManager) UpdateCache(ctx context.Context) error {
	var lastErr error
	for i := 0; i < pm.updateRetries; i++ {
		cmd := exec.CommandContext(ctx, "apt-get", "update")
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			lastErr = err
			// Exponential backoff
			delay := time.Duration(1<<uint(i)) * time.Second
			if delay > pm.maxRetryDelay {
				delay = pm.maxRetryDelay
			}
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("failed to update cache after %d attempts: %w", pm.updateRetries, lastErr)
}

// IsInstalled checks if a package is installed
func (pm *PackageManager) IsInstalled(ctx context.Context, pkg string) (bool, error) {
	cmd := exec.CommandContext(ctx, "dpkg", "-s", pkg)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("failed to check package status: %w", err)
	}
	return true, nil
}

// GetVersion gets the installed version of a package
func (pm *PackageManager) GetVersion(ctx context.Context, pkg string) (string, error) {
	cmd := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Version}", pkg)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get package version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// Option configures the package manager
type Option func(*PackageManager)

// WithDpkgOptions sets dpkg options
func WithDpkgOptions(opts []string) Option {
	return func(pm *PackageManager) {
		pm.dpkgOptions = opts
	}
}

// WithDefaultTimeout sets the default timeout
func WithDefaultTimeout(timeout time.Duration) Option {
	return func(pm *PackageManager) {
		pm.defaultTimeout = timeout
	}
}

func usage() {
	// Create package manager with options
	pm := NewPackageManager(
		WithDefaultTimeout(10*time.Minute),
		WithDpkgOptions([]string{"force-confdef", "force-confnew"}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Install packages
	err := pm.Install(ctx, []string{"nginx", "postgresql"}, PackageOptions{
		UpdateCache:          true,
		InstallRecommends:    ptr.Bool(false),
		AllowUnauthenticated: false,
		Force:                false,
	})
	if err != nil {
		log.Fatalf("Failed to install packages: %v", err)
	}

	// Remove packages
	err = pm.Remove(ctx, []string{"nginx"}, PackageOptions{
		Purge:      true,
		AutoRemove: true,
	})
	if err != nil {
		log.Fatalf("Failed to remove packages: %v", err)
	}
}
