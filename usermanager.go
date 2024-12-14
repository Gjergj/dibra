package main

import (
	"fmt"
	"strings"

	"github.com/Gjergj/dibra/pkg/commandexecutor"
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
type UserService struct {
	exec     *commandexecutor.CommandRunner
	WithSudo bool
}

// NewUserService creates a new UserService instance
func NewUserService(exec *commandexecutor.CommandRunner, withSudo bool) *UserService {
	return &UserService{exec: exec, WithSudo: withSudo}
}

// Exists checks if a user exists
func (s *UserService) Exists(username string) (bool, error) {
	cmd := commandexecutor.Command{
		Command:  "id",
		Args:     []string{username},
		WithSudo: s.WithSudo,
	}
	_, stderr, err := s.exec.Execute(cmd)
	// if err := cmd.Run(); err != nil {
	// 	// User doesn't exist or other error
	// 	return false, nil
	// }
	if err != nil {
		return false, nil
	}
	if stderr != "" {
		return false, fmt.Errorf("error checking user existence: %s", stderr)
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
		// Verify current state matches desired state
		currentUser, err := s.GetUserInfo(user.Username)
		if err != nil {
			return fmt.Errorf("error getting user info: %w", err)
		}

		if !s.userMatchesSpec(currentUser, user) {
			// Update user to match desired state
			return s.Update(user)
		}
		return nil
	}

	args := []string{"useradd"}

	if user.System {
		args = append(args, "--system")
	}
	if user.CreateHome {
		args = append(args, "--create-home")
	}
	if user.HomeDir != "" {
		args = append(args, "--create-home")
		args = append(args, "--home", user.HomeDir)
	}
	if user.Shell != "" {
		args = append(args, "--shell", user.Shell)
	}
	if len(user.Groups) > 0 {
		args = append(args, "--groups", strings.Join(user.Groups, ","))
	}
	args = append(args, user.Username)

	// cmd := exec.Command(args[0], args[1:]...)
	// if output, err := cmd.CombinedOutput(); err != nil {
	// 	return fmt.Errorf("error creating user: %w, output: %s", err, output)
	// }
	cmd := commandexecutor.Command{
		Command:  args[0],
		Args:     args[1:],
		WithSudo: s.WithSudo,
	}
	output, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return fmt.Errorf("error creating user: %w", err)
	}
	if stderr != "" {
		return fmt.Errorf("error creating user: %s", stderr)
	}
	fmt.Println(output)

	// Set password if provided
	if user.Password != "" {
		if err := s.setPassword(user.Username, user.Password); err != nil {
			return fmt.Errorf("error setting password: %w", err)
		}
	}

	return nil
}

// Helper method to compare user specifications
func (s *UserService) userMatchesSpec(current, desired User) bool {
	if desired.Shell != "" && current.Shell != desired.Shell {
		return false
	}
	if desired.HomeDir != "" && current.HomeDir != desired.HomeDir {
		return false
	}
	if len(desired.Groups) > 0 {
		currentGroups := make(map[string]bool)
		for _, g := range current.Groups {
			currentGroups[g] = true
		}
		for _, g := range desired.Groups {
			if !currentGroups[g] {
				return false
			}
		}
	}
	return true
}

// Helper method to get current user information
func (s *UserService) GetUserInfo(username string) (User, error) {
	var user User

	// Get user shell
	// shellCmd := exec.Command("getent", "passwd", username)
	// output, err := shellCmd.Output()
	cmd := commandexecutor.Command{
		Command:  "getent",
		Args:     []string{"passwd", username},
		WithSudo: s.WithSudo,
	}
	output, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return user, fmt.Errorf("error getting user info: %w", err)
	}
	if stderr != "" {
		return user, fmt.Errorf("error getting user info: %s", stderr)
	}
	output = strings.TrimSuffix(output, "\n")
	fields := strings.Split(string(output), ":")
	if len(fields) >= 7 {
		user.Username = fields[0]
		user.Shell = fields[6]
		user.HomeDir = fields[5]
	}

	// Get user groups
	// groupCmd := exec.Command("groups", username)
	// output, err = groupCmd.Output()
	cmd = commandexecutor.Command{
		Command:  "id",
		Args:     []string{"-Gn", username},
		WithSudo: s.WithSudo,
	}
	output, stderr, err = s.exec.Execute(cmd)
	if err != nil {
		return user, fmt.Errorf("error getting user groups: %w", err)
	}
	if stderr != "" {
		return user, fmt.Errorf("error getting user groups: %s", stderr)
	}

	output = strings.TrimSuffix(output, "\n")
	groups := strings.Split(string(output), " ")
	user.Groups = groups

	return user, nil
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

	// Get current state
	currentUser, err := s.GetUserInfo(user.Username)
	if err != nil {
		return fmt.Errorf("error getting user info: %w", err)
	}

	// Only update if there are actual changes
	if s.userMatchesSpec(currentUser, user) {
		return nil // No changes needed
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

	// cmd := exec.Command(args[0], args[1:]...)
	// if output, err := cmd.CombinedOutput(); err != nil {
	// 	return fmt.Errorf("error updating user: %w, output: %s", err, output)
	// }
	cmd := commandexecutor.Command{
		Command:  args[0],
		Args:     args[1:],
		WithSudo: s.WithSudo,
	}
	_, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return fmt.Errorf("error updating user: %w", err)
	}
	if stderr != "" {
		return fmt.Errorf("error updating user: %s", stderr)
	}

	// Update password if provided
	if user.Password != "" {
		if err := s.setPassword(user.Username, user.Password); err != nil {
			return fmt.Errorf("error updating password: %w", err)
		}
	}

	return nil
}

func (s *UserService) Delete(username string) error {
	exists, err := s.Exists(username)
	if err != nil {
		return fmt.Errorf("error checking user existence: %w", err)
	}
	if !exists {
		return nil // Already in desired state (non-existent)
	}

	// Ensure user is not logged in before deletion
	if isUserLoggedIn, err := s.isUserLoggedIn(username); err != nil {
		return fmt.Errorf("error checking user login status: %w", err)
	} else if isUserLoggedIn {
		return fmt.Errorf("user %s is currently logged in", username)
	}

	// cmd := exec.Command("userdel", username)
	// if output, err := cmd.CombinedOutput(); err != nil {
	// 	return fmt.Errorf("error deleting user: %w, output: %s", err, output)
	// }
	cmd := commandexecutor.Command{
		Command:  "userdel",
		Args:     []string{username},
		WithSudo: s.WithSudo,
	}
	_, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return fmt.Errorf("error deleting user: %w", err)
	}
	if stderr != "" {
		return fmt.Errorf("error deleting user: %s", stderr)
	}

	return nil
}

func (s *UserService) isUserLoggedIn(username string) (bool, error) {
	// cmd := exec.Command("who")
	// output, err := cmd.Output()
	cmd := commandexecutor.Command{
		Command:  "who",
		Args:     []string{},
		WithSudo: s.WithSudo,
	}
	output, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return false, fmt.Errorf("error checking logged-in users: %w", err)
	}
	if stderr != "" {
		return false, fmt.Errorf("error checking logged-in users: %s", stderr)
	}

	return strings.Contains(string(output), username), nil
}

// List returns all Linux users
func (s *UserService) List() ([]string, error) {
	// cmd := exec.Command("cut", "-d:", "-f1", "/etc/passwd")
	cmd := commandexecutor.Command{
		Command:  "cut",
		Args:     []string{"-d:", "-f1", "/etc/passwd"},
		WithSudo: s.WithSudo,
	}
	output, stderr, err := s.exec.Execute(cmd)
	// output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error listing users: %w", err)
	}
	if stderr != "" {
		return nil, fmt.Errorf("error listing users: %s", stderr)
	}

	users := strings.Split(strings.TrimSpace(string(output)), "\n")
	return users, nil
}

// setPassword is a helper function to set user password
func (s *UserService) setPassword(username, password string) error {
	// cmd := exec.Command("chpasswd")
	// cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", username, password))
	// if output, err := cmd.CombinedOutput(); err != nil {
	// 	return fmt.Errorf("error setting password: %w, output: %s", err, output)
	// }
	// Add basic password validation
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters long")
	}
	cmd := commandexecutor.Command{
		Command: "chpasswd\n",
		// Args:     []string{fmt.Sprintf("%s:%s", username, password)},
		WithSudo: s.WithSudo,
		Input:    fmt.Sprintf("%s:%s", username, password),
	}
	_, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return fmt.Errorf("error setting password: %w", err)
	}
	if stderr != "" {
		return fmt.Errorf("error setting password: %s", stderr)
	}
	return nil
}
