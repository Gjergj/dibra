//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_UserCreate(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser1"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create user
    user:
      name: testuser1
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	if !remoteUserExists(t, client, username) {
		t.Error("User should exist")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserCreateWithUID(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser2"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create user with specific UID
    user:
      name: testuser2
      uid: 2001
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	uid := remoteExec(t, client, "id -u "+username)
	if uid != "2001" {
		t.Errorf("Expected UID 2001, got %s", uid)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserCreateWithHomeDirectory(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser3"
	homeDir := "/opt/testuser3home"
	client.Run("userdel -rf " + username)
	client.Run("rm -rf " + homeDir)

	playbook := playbookHeader + `
  - name: Create user with custom home
    user:
      name: testuser3
      home: /opt/testuser3home
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	if !remoteDirExists(t, client, homeDir) {
		t.Error("Home directory should exist")
	}

	actualHome := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f6")
	if actualHome != homeDir {
		t.Errorf("Expected home %s, got %s", homeDir, actualHome)
	}

	client.Run("userdel -rf " + username)
	client.Run("rm -rf " + homeDir)
}

func TestPlaybook_UserCreateNoHome(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser4"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create user without home directory
    user:
      name: testuser4
      create_home: false
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	if !remoteUserExists(t, client, username) {
		t.Error("User should exist")
	}

	homeDir := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f6")
	if remoteDirExists(t, client, homeDir) {
		t.Error("Home directory should not exist when create_home=false")
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserCreateWithShell(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser5"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create user with specific shell
    user:
      name: testuser5
      shell: /bin/sh
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	shell := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f7")
	if shell != "/bin/sh" {
		t.Errorf("Expected shell /bin/sh, got %s", shell)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserCreateWithComment(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser6"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create user with comment
    user:
      name: testuser6
      comment: "Test User 6"
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	comment := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f5")
	if comment != "Test User 6" {
		t.Errorf("Expected comment 'Test User 6', got %s", comment)
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserCreateSystemUser(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testsysuser"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create system user
    user:
      name: testsysuser
      system: true
      shell: /usr/sbin/nologin
      create_home: false
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for system user creation")
	}

	uid := remoteExec(t, client, "id -u "+username)
	uidNum := 0
	if uid != "" {
		for _, c := range uid {
			if c >= '0' && c <= '9' {
				uidNum = uidNum*10 + int(c-'0')
			}
		}
	}
	if uidNum >= 1000 {
		t.Errorf("System user UID should be < 1000, got %d", uidNum)
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserRemove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser7"
	client.Run("useradd " + username)

	playbook := playbookHeader + `
  - name: Remove user
    user:
      name: testuser7
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user removal")
	}

	if remoteUserExists(t, client, username) {
		t.Error("User should not exist after removal")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (user already absent)")
	}
}

func TestPlaybook_UserRemoveWithHome(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser8"
	client.Run("userdel -rf " + username)
	client.Run("useradd -m " + username)

	homeDir := "/home/" + username
	if !remoteDirExists(t, client, homeDir) {
		t.Fatal("Home directory should exist before removal")
	}

	playbook := playbookHeader + `
  - name: Remove user with home directory
    user:
      name: testuser8
      state: absent
      remove: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user removal")
	}

	if remoteUserExists(t, client, username) {
		t.Error("User should not exist after removal")
	}

	if remoteDirExists(t, client, homeDir) {
		t.Error("Home directory should be removed with remove=true")
	}
}

func TestPlaybook_UserAddToGroup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser9"
	client.Run("userdel -rf " + username)
	client.Run("groupadd -f testgroup1")

	playbook := playbookHeader + `
  - name: Create user in specific group
    user:
      name: testuser9
      groups:
        - testgroup1
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	groups := remoteExec(t, client, "id -Gn "+username)
	if !strings.Contains(groups, "testgroup1") {
		t.Errorf("User should be in testgroup1, got groups: %s", groups)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("userdel -rf " + username)
	client.Run("groupdel testgroup1")
}

func TestPlaybook_UserAddToMultipleGroups(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser10"
	client.Run("userdel -rf " + username)
	client.Run("groupadd -f testgrp1")
	client.Run("groupadd -f testgrp2")
	client.Run("groupadd -f testgrp3")

	playbook := playbookHeader + `
  - name: Create user in multiple groups
    user:
      name: testuser10
      groups:
        - testgrp1
        - testgrp2
        - testgrp3
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	groups := remoteExec(t, client, "id -Gn "+username)
	for _, g := range []string{"testgrp1", "testgrp2", "testgrp3"} {
		if !strings.Contains(groups, g) {
			t.Errorf("User should be in %s, got groups: %s", g, groups)
		}
	}

	client.Run("userdel -rf " + username)
	client.Run("groupdel testgrp1")
	client.Run("groupdel testgrp2")
	client.Run("groupdel testgrp3")
}

func TestPlaybook_UserAppendToGroup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser11"
	client.Run("userdel -rf " + username)
	client.Run("groupadd -f grp_a")
	client.Run("groupadd -f grp_b")
	client.Run("useradd -m -G grp_a " + username)

	groups := remoteExec(t, client, "id -Gn "+username)
	if !strings.Contains(groups, "grp_a") {
		t.Fatalf("User should initially be in grp_a, got: %s", groups)
	}

	playbook := playbookHeader + `
  - name: Append user to additional group
    user:
      name: testuser11
      groups:
        - grp_b
      append: true
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group append")
	}

	groups = remoteExec(t, client, "id -Gn "+username)
	if !strings.Contains(groups, "grp_a") {
		t.Errorf("User should still be in grp_a after append, got: %s", groups)
	}
	if !strings.Contains(groups, "grp_b") {
		t.Errorf("User should now be in grp_b after append, got: %s", groups)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("userdel -rf " + username)
	client.Run("groupdel grp_a")
	client.Run("groupdel grp_b")
}

func TestPlaybook_UserReplaceGroups(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser12"
	client.Run("userdel -rf " + username)
	client.Run("groupadd -f old_grp")
	client.Run("groupadd -f new_grp")
	client.Run("useradd -m -G old_grp " + username)

	groups := remoteExec(t, client, "id -Gn "+username)
	if !strings.Contains(groups, "old_grp") {
		t.Fatalf("User should initially be in old_grp, got: %s", groups)
	}

	playbook := playbookHeader + `
  - name: Replace user groups (no append)
    user:
      name: testuser12
      groups:
        - new_grp
      append: false
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group replacement")
	}

	groups = remoteExec(t, client, "id -Gn "+username)
	if strings.Contains(groups, "old_grp") {
		t.Errorf("User should NOT be in old_grp after replace, got: %s", groups)
	}
	if !strings.Contains(groups, "new_grp") {
		t.Errorf("User should be in new_grp after replace, got: %s", groups)
	}

	client.Run("userdel -rf " + username)
	client.Run("groupdel old_grp")
	client.Run("groupdel new_grp")
}

func TestPlaybook_UserWithPrimaryGroup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser13"
	client.Run("userdel -rf " + username)
	client.Run("groupadd -f myprimary")

	playbook := playbookHeader + `
  - name: Create user with specific primary group
    user:
      name: testuser13
      group: myprimary
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	primaryGroup := remoteExec(t, client, "id -gn "+username)
	if primaryGroup != "myprimary" {
		t.Errorf("Expected primary group myprimary, got %s", primaryGroup)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("userdel -rf " + username)
	client.Run("groupdel myprimary")
}

func TestPlaybook_UserPasswordLock(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser14"
	client.Run("userdel -rf " + username)
	client.Run("useradd -m " + username)
	client.Run("echo '" + username + ":testpass' | chpasswd")

	playbook := playbookHeader + `
  - name: Lock user password
    user:
      name: testuser14
      password_lock: true
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for password lock")
	}

	status := remoteExec(t, client, "passwd -S "+username)
	if !strings.Contains(status, " L ") && !strings.Contains(status, " LK ") {
		t.Errorf("Password should be locked, got status: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserPasswordUnlock(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser15"
	client.Run("userdel -rf " + username)
	client.Run("useradd -m " + username)
	client.Run("echo '" + username + ":testpass' | chpasswd")
	client.Run("usermod -L " + username)

	status := remoteExec(t, client, "passwd -S "+username)
	if !strings.Contains(status, " L ") && !strings.Contains(status, " LK ") {
		t.Fatalf("Password should be locked initially, got: %s", status)
	}

	playbook := playbookHeader + `
  - name: Unlock user password
    user:
      name: testuser15
      password_lock: false
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for password unlock")
	}

	status = remoteExec(t, client, "passwd -S "+username)
	if strings.Contains(status, " L ") || strings.Contains(status, " LK ") {
		t.Errorf("Password should be unlocked, got status: %s", status)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserModifyShell(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser16"
	client.Run("userdel -rf " + username)
	client.Run("useradd -m -s /bin/bash " + username)

	shell := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f7")
	if shell != "/bin/bash" {
		t.Fatalf("Initial shell should be /bin/bash, got: %s", shell)
	}

	playbook := playbookHeader + `
  - name: Change user shell
    user:
      name: testuser16
      shell: /bin/sh
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for shell change")
	}

	shell = remoteExec(t, client, "getent passwd "+username+" | cut -d: -f7")
	if shell != "/bin/sh" {
		t.Errorf("Shell should be /bin/sh, got: %s", shell)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserModifyComment(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser17"
	client.Run("userdel -rf " + username)
	client.Run("useradd -m -c 'Old Comment' " + username)

	playbook := playbookHeader + `
  - name: Change user comment
    user:
      name: testuser17
      comment: "New Comment"
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for comment change")
	}

	comment := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f5")
	if comment != "New Comment" {
		t.Errorf("Comment should be 'New Comment', got: %s", comment)
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserGenerateSSHKey(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser18"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create user with SSH key
    user:
      name: testuser18
      generate_ssh_key: true
      ssh_key_type: rsa
      ssh_key_bits: 2048
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation with SSH key")
	}

	homeDir := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f6")
	sshDir := homeDir + "/.ssh"
	privateKey := sshDir + "/id_rsa"
	publicKey := sshDir + "/id_rsa.pub"

	if !remoteDirExists(t, client, sshDir) {
		t.Error(".ssh directory should exist")
	}

	if !remoteFileExists(t, client, privateKey) {
		t.Error("Private key should exist")
	}

	if !remoteFileExists(t, client, publicKey) {
		t.Error("Public key should exist")
	}

	privKeyMode := remoteFileMode(t, client, privateKey)
	if privKeyMode != "600" {
		t.Errorf("Private key mode should be 600, got: %s", privKeyMode)
	}

	privKeyOwner := remoteFileOwner(t, client, privateKey)
	if privKeyOwner != username {
		t.Errorf("Private key owner should be %s, got: %s", username, privKeyOwner)
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserSSHKeyIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser19"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create user with SSH key
    user:
      name: testuser19
      generate_ssh_key: true
      state: present
`
	output := runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for initial creation")
	}

	homeDir := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f6")
	originalKey := remoteFileContent(t, client, homeDir+"/.ssh/id_rsa.pub")

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (key exists)")
	}

	currentKey := remoteFileContent(t, client, homeDir+"/.ssh/id_rsa.pub")
	if originalKey != currentKey {
		t.Error("SSH key should not be regenerated without force=true")
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserSSHKeyForceRegenerate(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser20"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create user with SSH key
    user:
      name: testuser20
      generate_ssh_key: true
      state: present
`
	runPlaybook(t, playbook)

	homeDir := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f6")
	originalKey := remoteFileContent(t, client, homeDir+"/.ssh/id_rsa.pub")

	playbookForce := playbookHeader + `
  - name: Regenerate SSH key with force
    user:
      name: testuser20
      generate_ssh_key: true
      force: true
      state: present
`
	output := runPlaybook(t, playbookForce)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED when force regenerating key")
	}

	currentKey := remoteFileContent(t, client, homeDir+"/.ssh/id_rsa.pub")
	if originalKey == currentKey {
		t.Error("SSH key should be regenerated with force=true")
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserWithPassword(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser21"
	client.Run("userdel -rf " + username)

	hash := remoteExec(t, client, "openssl passwd -6 'testpassword123'")

	playbook := playbookHeader + `
  - name: Create user with password
    user:
      name: testuser21
      password: "` + hash + `"
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	shadow := remoteExec(t, client, "getent shadow "+username)
	if !strings.Contains(shadow, "$6$") {
		t.Error("User should have SHA512 password hash set")
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserRemoveFromAllGroups(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "testuser22"
	client.Run("userdel -rf " + username)
	client.Run("groupadd -f extragrp1")
	client.Run("groupadd -f extragrp2")
	client.Run("useradd -m -G extragrp1,extragrp2 " + username)

	groups := remoteExec(t, client, "id -Gn "+username)
	if !strings.Contains(groups, "extragrp1") || !strings.Contains(groups, "extragrp2") {
		t.Fatalf("User should be in extragrp1,extragrp2 initially, got: %s", groups)
	}

	playbook := playbookHeader + `
  - name: Remove user from all supplementary groups
    user:
      name: testuser22
      groups: []
      append: false
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group removal")
	}

	groups = remoteExec(t, client, "id -Gn "+username)
	if strings.Contains(groups, "extragrp1") || strings.Contains(groups, "extragrp2") {
		t.Errorf("User should not be in any supplementary groups, got: %s", groups)
	}

	client.Run("userdel -rf " + username)
	client.Run("groupdel extragrp1")
	client.Run("groupdel extragrp2")
}

func TestPlaybook_UserFullWorkflow(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "devuser"
	client.Run("userdel -rf " + username)
	client.Run("groupadd -f developers")
	client.Run("groupadd -f docker")

	playbook := playbookHeader + `
  - name: Create developer user
    user:
      name: devuser
      uid: 3001
      comment: "Developer User"
      shell: /bin/bash
      home: /home/devuser
      groups:
        - developers
        - docker
      generate_ssh_key: true
      ssh_key_type: rsa
      ssh_key_bits: 4096
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	uid := remoteExec(t, client, "id -u "+username)
	if uid != "3001" {
		t.Errorf("Expected UID 3001, got %s", uid)
	}

	comment := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f5")
	if comment != "Developer User" {
		t.Errorf("Expected comment 'Developer User', got %s", comment)
	}

	shell := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f7")
	if shell != "/bin/bash" {
		t.Errorf("Expected shell /bin/bash, got %s", shell)
	}

	groups := remoteExec(t, client, "id -Gn "+username)
	if !strings.Contains(groups, "developers") {
		t.Errorf("User should be in developers group, got: %s", groups)
	}
	if !strings.Contains(groups, "docker") {
		t.Errorf("User should be in docker group, got: %s", groups)
	}

	if !remoteFileExists(t, client, "/home/devuser/.ssh/id_rsa") {
		t.Error("SSH private key should exist")
	}
	if !remoteFileExists(t, client, "/home/devuser/.ssh/id_rsa.pub") {
		t.Error("SSH public key should exist")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("userdel -rf " + username)
	client.Run("groupdel developers")
	client.Run("groupdel docker")
}

func TestPlaybook_UserAddToSudoGroup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "sudouser"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create user with sudo access
    user:
      name: sudouser
      groups:
        - sudo
      append: true
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	groups := remoteExec(t, client, "id -Gn "+username)
	if !strings.Contains(groups, "sudo") {
		t.Errorf("User should be in sudo group, got: %s", groups)
	}

	client.Run("userdel -rf " + username)
}

func TestPlaybook_UserNologinShell(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	username := "serviceuser"
	client.Run("userdel -rf " + username)

	playbook := playbookHeader + `
  - name: Create service user with nologin shell
    user:
      name: serviceuser
      shell: /usr/sbin/nologin
      system: true
      create_home: false
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	shell := remoteExec(t, client, "getent passwd "+username+" | cut -d: -f7")
	if shell != "/usr/sbin/nologin" {
		t.Errorf("Expected shell /usr/sbin/nologin, got %s", shell)
	}

	client.Run("userdel -rf " + username)
}

func remoteUserExists(t *testing.T, client interface{ Run(string) (string, string, error) }, username string) bool {
	_, _, err := client.Run("id " + username)
	return err == nil
}
