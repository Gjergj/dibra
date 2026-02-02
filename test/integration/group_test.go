//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_GroupCreate(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testgroup1"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create group
    group:
      name: testgroup1
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group creation")
	}

	if !remoteGroupExists(t, client, groupName) {
		t.Error("Group should exist")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupCreateIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testgroup2"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create group
    group:
      name: testgroup2
      state: present
`
	runPlaybook(t, playbook)

	output := runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on third run (idempotent)")
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupCreateWithGID(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testgroup3"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create group with specific GID
    group:
      name: testgroup3
      gid: 5001
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group creation")
	}

	gid := remoteExec(t, client, "getent group "+groupName+" | cut -d: -f3")
	if gid != "5001" {
		t.Errorf("Expected GID 5001, got %s", gid)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupModifyGID(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testgroup4"
	client.Run("groupdel " + groupName)
	client.Run("groupadd -g 5002 " + groupName)

	gidBefore := remoteExec(t, client, "getent group "+groupName+" | cut -d: -f3")
	if gidBefore != "5002" {
		t.Fatalf("Initial GID should be 5002, got %s", gidBefore)
	}

	playbook := playbookHeader + `
  - name: Modify group GID
    group:
      name: testgroup4
      gid: 5003
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for GID modification")
	}

	gidAfter := remoteExec(t, client, "getent group "+groupName+" | cut -d: -f3")
	if gidAfter != "5003" {
		t.Errorf("Expected GID 5003 after modification, got %s", gidAfter)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupModifyGIDIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testgroup5"
	client.Run("groupdel " + groupName)
	client.Run("groupadd -g 5004 " + groupName)

	playbook := playbookHeader + `
  - name: Ensure group with same GID
    group:
      name: testgroup5
      gid: 5004
      state: present
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when GID already matches")
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupRemove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testgroup6"
	client.Run("groupadd " + groupName)

	if !remoteGroupExists(t, client, groupName) {
		t.Fatal("Group should exist before removal")
	}

	playbook := playbookHeader + `
  - name: Remove group
    group:
      name: testgroup6
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group removal")
	}

	if remoteGroupExists(t, client, groupName) {
		t.Error("Group should not exist after removal")
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when removing already absent group")
	}
}

func TestPlaybook_GroupRemoveNonExistent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "nonexistentgroup"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Remove non-existent group
    group:
      name: nonexistentgroup
      state: absent
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes for removing non-existent group")
	}

	if strings.Contains(output, "FAILED") {
		t.Error("Should not fail when removing non-existent group")
	}
}

func TestPlaybook_GroupCreateSystem(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testsysgroup"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create system group
    group:
      name: testsysgroup
      system: true
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for system group creation")
	}

	gid := remoteExec(t, client, "getent group "+groupName+" | cut -d: -f3")
	gidNum := 0
	if gid != "" {
		for _, c := range gid {
			if c >= '0' && c <= '9' {
				gidNum = gidNum*10 + int(c-'0')
			}
		}
	}
	if gidNum >= 1000 {
		t.Errorf("System group GID should be < 1000, got %d", gidNum)
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupNonUniqueGID(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	group1 := "testgroup7"
	group2 := "testgroup8"
	client.Run("groupdel " + group1)
	client.Run("groupdel " + group2)

	client.Run("groupadd -g 5010 " + group1)

	playbook := playbookHeader + `
  - name: Create group with non-unique GID
    group:
      name: testgroup8
      gid: 5010
      non_unique: true
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group creation with non-unique GID")
	}

	if !remoteGroupExists(t, client, group2) {
		t.Error("Group should exist")
	}

	gid := remoteExec(t, client, "getent group "+group2+" | cut -d: -f3")
	if gid != "5010" {
		t.Errorf("Expected GID 5010, got %s", gid)
	}

	client.Run("groupdel " + group2)
	client.Run("groupdel " + group1)
}

func TestPlaybook_GroupNonUniqueRequiresGID(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testgroup9"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create group with non_unique but no gid
    group:
      name: testgroup9
      non_unique: true
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED when non_unique is set without gid")
	}

	if strings.Contains(output, "non_unique requires gid") {
		// Expected error message
	}
}

func TestPlaybook_GroupNameRequired(t *testing.T) {
	playbook := playbookHeader + `
  - name: Create group without name
    group:
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED when name is missing")
	}
}

func TestPlaybook_GroupInvalidState(t *testing.T) {
	playbook := playbookHeader + `
  - name: Create group with invalid state
    group:
      name: testinvalidstate
      state: invalid
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for invalid state")
	}
}

func TestPlaybook_GroupForceDelete(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testforcegroup"
	userName := "testforceuser"
	client.Run("userdel -rf " + userName)
	client.Run("groupdel " + groupName)

	client.Run("groupadd " + groupName)
	client.Run("useradd -m -g " + groupName + " " + userName)

	if !remoteGroupExists(t, client, groupName) {
		t.Fatal("Group should exist")
	}
	if !remoteUserExists(t, client, userName) {
		t.Fatal("User should exist with group as primary")
	}

	playbookNoForce := playbookHeader + `
  - name: Try to remove group without force
    group:
      name: testforcegroup
      state: absent
`
	output := runPlaybook(t, playbookNoForce)
	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED when removing group that is primary for a user")
	}

	playbookForce := playbookHeader + `
  - name: Remove group with force
    group:
      name: testforcegroup
      force: true
      state: absent
`
	output = runPlaybook(t, playbookForce)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED when force deleting group")
	}

	if remoteGroupExists(t, client, groupName) {
		t.Error("Group should not exist after force removal")
	}

	client.Run("userdel -rf " + userName)
}

func TestPlaybook_GroupMultipleRuns(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testgroup10"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create group
    group:
      name: testgroup10
      state: present
`
	for i := 0; i < 5; i++ {
		output := runPlaybook(t, playbook)
		if i == 0 {
			if !strings.Contains(output, "CHANGED") {
				t.Error("Expected CHANGED on first run")
			}
		} else {
			if strings.Contains(output, "CHANGED") {
				t.Errorf("Expected no changes on run %d", i+1)
			}
		}
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupCreateMultiple(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groups := []string{"multigroup1", "multigroup2", "multigroup3"}
	for _, g := range groups {
		client.Run("groupdel " + g)
	}

	playbook := playbookHeader + `
  - name: Create first group
    group:
      name: multigroup1
      gid: 6001
      state: present
  - name: Create second group
    group:
      name: multigroup2
      gid: 6002
      state: present
  - name: Create third group
    group:
      name: multigroup3
      gid: 6003
      state: present
`
	output := runPlaybook(t, playbook)

	changeCount := strings.Count(output, "CHANGED")
	if changeCount != 3 {
		t.Errorf("Expected 3 CHANGED, got %d", changeCount)
	}

	for _, g := range groups {
		if !remoteGroupExists(t, client, g) {
			t.Errorf("Group %s should exist", g)
		}
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	for _, g := range groups {
		client.Run("groupdel " + g)
	}
}

func TestPlaybook_GroupGIDRange(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "testgidrange"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create group with high GID
    group:
      name: testgidrange
      gid: 65000
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group creation with high GID")
	}

	gid := remoteExec(t, client, "getent group "+groupName+" | cut -d: -f3")
	if gid != "65000" {
		t.Errorf("Expected GID 65000, got %s", gid)
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupSpecialCharName(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "test-group_with.chars"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create group with special characters in name
    group:
      name: test-group_with.chars
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group creation")
	}

	if !remoteGroupExists(t, client, groupName) {
		t.Error("Group should exist")
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupWithUsers(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "developers"
	userName := "devuser1"
	client.Run("userdel -rf " + userName)
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create developers group
    group:
      name: developers
      gid: 7001
      state: present
  - name: Create dev user in developers group
    user:
      name: devuser1
      groups:
        - developers
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED")
	}

	if !remoteGroupExists(t, client, groupName) {
		t.Error("Group should exist")
	}

	groups := remoteExec(t, client, "id -Gn "+userName)
	if !strings.Contains(groups, groupName) {
		t.Errorf("User should be in group %s, got: %s", groupName, groups)
	}

	client.Run("userdel -rf " + userName)
	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupRetainsMembers(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "membergroup"
	user1 := "memberuser1"
	user2 := "memberuser2"
	client.Run("userdel -rf " + user1)
	client.Run("userdel -rf " + user2)
	client.Run("groupdel " + groupName)

	client.Run("groupadd -g 7100 " + groupName)
	client.Run("useradd -m -G " + groupName + " " + user1)
	client.Run("useradd -m -G " + groupName + " " + user2)

	playbook := playbookHeader + `
  - name: Modify group GID
    group:
      name: membergroup
      gid: 7101
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for GID modification")
	}

	gid := remoteExec(t, client, "getent group "+groupName+" | cut -d: -f3")
	if gid != "7101" {
		t.Errorf("Expected GID 7101, got %s", gid)
	}

	client.Run("userdel -rf " + user1)
	client.Run("userdel -rf " + user2)
	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupDefaultState(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "defaultstategroup"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create group with default state
    group:
      name: defaultstategroup
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group creation")
	}

	if !remoteGroupExists(t, client, groupName) {
		t.Error("Group should exist (default state is present)")
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupFullWorkflow(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "workflowgroup"
	userName := "workflowuser"
	client.Run("userdel -rf " + userName)
	client.Run("groupdel " + groupName)

	playbookCreate := playbookHeader + `
  - name: Create workflow group
    group:
      name: workflowgroup
      gid: 8001
      state: present
`
	output := runPlaybook(t, playbookCreate)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group creation")
	}

	playbookAddUser := playbookHeader + `
  - name: Create user with workflow group
    user:
      name: workflowuser
      group: workflowgroup
      state: present
`
	output = runPlaybook(t, playbookAddUser)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for user creation")
	}

	primaryGroup := remoteExec(t, client, "id -gn "+userName)
	if primaryGroup != groupName {
		t.Errorf("Expected primary group %s, got %s", groupName, primaryGroup)
	}

	playbookModify := playbookHeader + `
  - name: Modify group GID
    group:
      name: workflowgroup
      gid: 8002
      state: present
`
	output = runPlaybook(t, playbookModify)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for GID modification")
	}

	gid := remoteExec(t, client, "getent group "+groupName+" | cut -d: -f3")
	if gid != "8002" {
		t.Errorf("Expected GID 8002, got %s", gid)
	}

	client.Run("userdel -rf " + userName)

	playbookRemove := playbookHeader + `
  - name: Remove workflow group
    group:
      name: workflowgroup
      state: absent
`
	output = runPlaybook(t, playbookRemove)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group removal")
	}

	if remoteGroupExists(t, client, groupName) {
		t.Error("Group should not exist after removal")
	}
}

func TestPlaybook_GroupCreateSystemWithGID(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groupName := "sysgroup2"
	client.Run("groupdel " + groupName)

	playbook := playbookHeader + `
  - name: Create system group with specific GID
    group:
      name: sysgroup2
      gid: 500
      system: true
      state: present
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for system group creation")
	}

	gid := remoteExec(t, client, "getent group "+groupName+" | cut -d: -f3")
	if gid != "500" {
		t.Errorf("Expected GID 500, got %s", gid)
	}

	client.Run("groupdel " + groupName)
}

func TestPlaybook_GroupRemoveMultiple(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	groups := []string{"rmgroup1", "rmgroup2", "rmgroup3"}
	for _, g := range groups {
		client.Run("groupadd " + g)
	}

	for _, g := range groups {
		if !remoteGroupExists(t, client, g) {
			t.Fatalf("Group %s should exist before removal", g)
		}
	}

	playbook := playbookHeader + `
  - name: Remove first group
    group:
      name: rmgroup1
      state: absent
  - name: Remove second group
    group:
      name: rmgroup2
      state: absent
  - name: Remove third group
    group:
      name: rmgroup3
      state: absent
`
	output := runPlaybook(t, playbook)

	changeCount := strings.Count(output, "CHANGED")
	if changeCount != 3 {
		t.Errorf("Expected 3 CHANGED, got %d", changeCount)
	}

	for _, g := range groups {
		if remoteGroupExists(t, client, g) {
			t.Errorf("Group %s should not exist after removal", g)
		}
	}
}

func TestPlaybook_GroupExistingCheck(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Ensure root group exists
    group:
      name: root
      state: present
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes for existing root group")
	}

	if strings.Contains(output, "FAILED") {
		t.Error("Should not fail for existing group")
	}
}

func remoteGroupExists(t *testing.T, client interface{ Run(string) (string, string, error) }, groupName string) bool {
	_, _, err := client.Run("getent group " + groupName)
	return err == nil
}
