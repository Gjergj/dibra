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

func (s *UserService) createGroup(groupName string) error {

	exists, err := s.groupExists(groupName)
	if err != nil {
		return fmt.Errorf("error checking group existence: %w", err)
	}
	if exists {
		return nil
	}

	cmd := commandexecutor.Command{
		Command:  "groupadd",
		Args:     []string{"--system", groupName},
		WithSudo: s.WithSudo,
	}
	_, err = s.exec.ExecuteCombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("error creating group: %w", err)
	}

	return nil
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

	args = append(args, user.Username)

	if user.System {
		args = append(args, "--system")
	}
	if user.CreateHome {
		args = append(args, "--create-home")
	} else {
		args = append(args, "--no-create-home")
	}
	if user.HomeDir != "" {
		args = append(args, "--home", user.HomeDir)
	}
	if user.Shell == "bash" {
		args = append(args, "--shell", "/bin/bash")
	} else if user.Shell != "" {
		args = append(args, "--shell", user.Shell)
	} else if user.System {
		args = append(args, "--shell", "/usr/sbin/nologin")
	}
	if len(user.Groups) > 0 {
		args = append(args, "-g", user.Groups[0])
		if len(user.Groups) > 1 {
			args = append(args, "--groups", strings.Join(user.Groups[1:], ","))
		}
	}
	if user.System {

		if err := s.createGroup(user.Username); err != nil {
			return fmt.Errorf("error creating group: %w", err)
		}
	}
	// cmd := exec.Command(args[0], args[1:]...)
	// if output, err := cmd.CombinedOutput(); err != nil {
	// 	return fmt.Errorf("error creating user: %w, output: %s", err, output)
	// }
	cmd := commandexecutor.Command{
		Command:  args[0],
		Args:     args[1:],
		WithSudo: s.WithSudo,
	}
	output, err := s.exec.ExecuteCombinedOutput(cmd)
	if err != nil {
		fmt.Println(output)
		return fmt.Errorf("error creating user: %w", err)
	}

	// Set password if provided
	if user.Password != "" {
		if err := s.setPassword(user.Username, user.Password); err != nil {
			return fmt.Errorf("error setting password: %w", err)
		}
	}

	// Handle group assignments
	if err := s.updateGroups(user.Username, user.Groups); err != nil {
		return fmt.Errorf("error setting user groups: %w", err)
	}

	return nil
}

// Helper method to update user groups
func (s *UserService) updateGroups(username string, desiredGroups []string) error {
	// First, get current supplementary groups
	currentUser, err := s.GetUserInfo(username)
	if err != nil {
		return fmt.Errorf("error getting current user info: %w", err)
	}

	// Get primary group
	primaryGroup, err := s.getPrimaryGroup(username)
	if err != nil {
		return fmt.Errorf("error getting primary group: %w", err)
	}

	// Check if groups are already in desired state
	if s.groupsMatch(currentUser.Groups, desiredGroups) {
		return nil // Already in desired state
	}

	// Verify all groups exist before making any changes
	for _, group := range desiredGroups {
		if exists, err := s.groupExists(group); err != nil {
			return fmt.Errorf("error checking group existence: %w", err)
		} else if !exists {
			return fmt.Errorf("group %s does not exist", group)
		}
	}

	// Build new groups list, ensuring primary group is preserved
	newGroups := make([]string, 0, len(desiredGroups)+1)
	groupSet := make(map[string]bool)

	// Always include primary group first
	newGroups = append(newGroups, primaryGroup)
	groupSet[primaryGroup] = true

	// Add other desired groups, avoiding duplicates
	for _, group := range desiredGroups {
		if !groupSet[group] {
			newGroups = append(newGroups, group)
			groupSet[group] = true
		}
	}

	// Only update if there are supplementary groups to set
	if len(newGroups) > 1 {
		// Use -G for supplementary groups (excludes primary group)
		args := []string{"usermod", "-G", strings.Join(newGroups[1:], ","), username}
		cmd := commandexecutor.Command{
			Command:  args[0],
			Args:     args[1:],
			WithSudo: s.WithSudo,
		}
		_, err = s.exec.ExecuteCombinedOutput(cmd)
		if err != nil {
			return fmt.Errorf("error updating groups: %w", err)
		}
	}

	return nil
}

// Helper method to check if a group exists
func (s *UserService) groupExists(groupName string) (bool, error) {
	cmd := commandexecutor.Command{
		Command:  "getent",
		Args:     []string{"group", groupName},
		WithSudo: s.WithSudo,
	}
	output, err := s.exec.ExecuteCombinedOutput(cmd)
	if err != nil {
		return false, fmt.Errorf("error checking group existence: %w", err)
	}
	fields := strings.Split(output, ":")
	if len(fields) > 1 && fields[0] == groupName {
		return true, nil
	}
	return false, nil
}

// Improved group matching that handles primary groups correctly
func (s *UserService) groupsMatch(current, desired []string) bool {
	if len(current) < len(desired) {
		return false
	}

	currentMap := make(map[string]bool)
	for _, g := range current {
		currentMap[g] = true
	}

	// Check if all desired groups are in current groups
	for _, g := range desired {
		if !currentMap[g] {
			return false
		}
	}

	return true
}

// Helper method to get user's primary group
func (s *UserService) getPrimaryGroup(username string) (string, error) {
	cmd := commandexecutor.Command{
		Command:  "id",
		Args:     []string{"-gn", username},
		WithSudo: s.WithSudo,
	}
	output, err := s.exec.ExecuteCombinedOutput(cmd)
	if err != nil {
		return "", fmt.Errorf("error getting primary group: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
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
