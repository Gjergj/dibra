package commandexecutor

import (
	"fmt"
	"os/exec"
	"strings"
)

// User represents a Linux user account
type User struct {
	Username   string
	Password   string   // Optional: encrypted password
	Shell      string   // Default: /bin/bash
	HomeDir    string   // Optional: custom home directory
	Groups     []string // Optional: additional groups
	System     bool     // If true, creates a system user
	CreateHome bool     // If true, creates home directory
}

// UserService handles Linux user operations
type UserService struct{}

// NewUserService creates a new UserService instance
func NewUserService() *UserService {
	return &UserService{}
}

// Exists checks if a user exists
func (s *UserService) Exists(username string) (bool, error) {
	cmd := exec.Command("id", username)
	if err := cmd.Run(); err != nil {
		// User doesn't exist or other error
		return false, nil
	}
	return true, nil
}

// Create creates a new Linux user if it doesn't exist
func (s *UserService) Create(user User) error {
	exists, err := s.Exists(user.Username)
	if err != nil {
		return fmt.Errorf("error checking user existence: %w", err)
	}
	if exists {
		return nil // Idempotency: user already exists
	}

	args := []string{"useradd"}

	if user.System {
		args = append(args, "--system")
	}
	if user.CreateHome {
		args = append(args, "--create-home")
	}
	if user.HomeDir != "" {
		args = append(args, "--home", user.HomeDir)
	}
	if user.Shell != "" {
		args = append(args, "--shell", user.Shell)
	}
	if len(user.Groups) > 0 {
		args = append(args, "--groups", strings.Join(user.Groups, ","))
	}
	args = append(args, user.Username)

	cmd := exec.Command(args[0], args[1:]...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error creating user: %w, output: %s", err, output)
	}

	// Set password if provided
	if user.Password != "" {
		if err := s.setPassword(user.Username, user.Password); err != nil {
			return fmt.Errorf("error setting password: %w", err)
		}
	}

	return nil
}

// Update updates an existing Linux user
func (s *UserService) Update(user User) error {
	exists, err := s.Exists(user.Username)
	if err != nil {
		return fmt.Errorf("error checking user existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("user %s does not exist", user.Username)
	}

	args := []string{"usermod"}

	if user.HomeDir != "" {
		args = append(args, "--home", user.HomeDir)
	}
	if user.Shell != "" {
		args = append(args, "--shell", user.Shell)
	}
	if len(user.Groups) > 0 {
		args = append(args, "--groups", strings.Join(user.Groups, ","))
	}
	args = append(args, user.Username)

	cmd := exec.Command(args[0], args[1:]...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error updating user: %w, output: %s", err, output)
	}

	// Update password if provided
	if user.Password != "" {
		if err := s.setPassword(user.Username, user.Password); err != nil {
			return fmt.Errorf("error updating password: %w", err)
		}
	}

	return nil
}

// Delete removes a Linux user if it exists
func (s *UserService) Delete(username string) error {
	exists, err := s.Exists(username)
	if err != nil {
		return fmt.Errorf("error checking user existence: %w", err)
	}
	if !exists {
		return nil // Idempotency: user doesn't exist
	}

	cmd := exec.Command("userdel", username)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error deleting user: %w, output: %s", err, output)
	}

	return nil
}

// List returns all Linux users
func (s *UserService) List() ([]string, error) {
	cmd := exec.Command("cut", "-d:", "-f1", "/etc/passwd")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error listing users: %w", err)
	}

	users := strings.Split(strings.TrimSpace(string(output)), "\n")
	return users, nil
}

// setPassword is a helper function to set user password
func (s *UserService) setPassword(username, password string) error {
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", username, password))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error setting password: %w, output: %s", err, output)
	}
	return nil
}
