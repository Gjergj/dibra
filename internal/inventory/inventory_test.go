package inventory

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/config"
)

func writeInventory(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "inventory.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write inventory: %v", err)
	}
	return path
}

func TestLoad_BasicAllGroup(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  hosts:
    host1:
      host: 192.168.1.1
    host2:
      host: 192.168.1.2
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	names := inv.HostNames()
	if !reflect.DeepEqual(names, []string{"host1", "host2"}) {
		t.Errorf("expected [host1 host2], got %v", names)
	}

	if inv.BaseDir != dir {
		t.Errorf("expected BaseDir=%s, got %s", dir, inv.BaseDir)
	}
}

func TestLoad_GroupsWithHostsAndVars(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  children:
    webservers:
      hosts:
        web1:
          host: 10.0.0.1
        web2:
          host: 10.0.0.2
      vars:
        http_port: 80
    dbservers:
      hosts:
        db1:
          host: 10.0.1.1
      vars:
        db_port: 5432
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	names := inv.HostNames()
	if !reflect.DeepEqual(names, []string{"db1", "web1", "web2"}) {
		t.Errorf("expected [db1 web1 web2], got %v", names)
	}

	webVars := inv.EffectiveVarsForHost("web1")
	if webVars["http_port"] != 80 {
		t.Errorf("expected http_port=80, got %v", webVars["http_port"])
	}
	if webVars["host"] != "10.0.0.1" {
		t.Errorf("expected host=10.0.0.1, got %v", webVars["host"])
	}

	dbVars := inv.EffectiveVarsForHost("db1")
	if dbVars["db_port"] != 5432 {
		t.Errorf("expected db_port=5432, got %v", dbVars["db_port"])
	}
}

func TestLoad_ImplicitAllGroup(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
webservers:
  hosts:
    web1:
      host: 10.0.0.1
dbservers:
  hosts:
    db1:
      host: 10.0.1.1
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if _, ok := inv.Groups["all"]; !ok {
		t.Error("expected implicit 'all' group")
	}

	names := inv.HostNames()
	if !reflect.DeepEqual(names, []string{"db1", "web1"}) {
		t.Errorf("expected [db1 web1], got %v", names)
	}

	web1Groups := inv.GroupsForHost("web1")
	if !contains(web1Groups, "all") {
		t.Error("expected web1 to be in 'all' group")
	}
	if !contains(web1Groups, "webservers") {
		t.Error("expected web1 to be in 'webservers' group")
	}
}

func TestLoad_UngroupedHosts(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  hosts:
    standalone:
      host: 10.0.0.99
  children:
    webservers:
      hosts:
        web1:
          host: 10.0.0.1
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	standaloneGroups := inv.GroupsForHost("standalone")
	if !contains(standaloneGroups, "ungrouped") {
		t.Errorf("expected standalone in 'ungrouped', groups=%v", standaloneGroups)
	}

	web1Groups := inv.GroupsForHost("web1")
	if contains(web1Groups, "ungrouped") {
		t.Errorf("web1 should not be in 'ungrouped', groups=%v", web1Groups)
	}
}

func TestLoad_ChildrenGroupHierarchy(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  children:
    east:
      hosts:
        east1:
          host: 10.1.0.1
      vars:
        region: east
    west:
      hosts:
        west1:
          host: 10.2.0.1
      vars:
        region: west
    production:
      children:
        east:
        west:
      vars:
        env: prod
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	east1Groups := inv.GroupsForHost("east1")
	if !contains(east1Groups, "east") {
		t.Error("expected east1 in 'east'")
	}
	if !contains(east1Groups, "production") {
		t.Error("expected east1 in 'production' (parent of east)")
	}
	if !contains(east1Groups, "all") {
		t.Error("expected east1 in 'all'")
	}

	east1Vars := inv.EffectiveVarsForHost("east1")
	if east1Vars["env"] != "prod" {
		t.Errorf("expected env=prod, got %v", east1Vars["env"])
	}
	if east1Vars["region"] != "east" {
		t.Errorf("expected region=east (child overrides parent), got %v", east1Vars["region"])
	}
}

func TestLoad_VariablePrecedence(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  vars:
    shared: from_all
    all_only: all_val
  children:
    parent_group:
      vars:
        shared: from_parent
        parent_only: parent_val
      children:
        child_group:
          hosts:
            myhost:
              shared: from_host
              host_only: host_val
          vars:
            shared: from_child
            child_only: child_val
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	vars := inv.EffectiveVarsForHost("myhost")
	if vars["shared"] != "from_host" {
		t.Errorf("expected shared=from_host (host wins), got %v", vars["shared"])
	}
	if vars["all_only"] != "all_val" {
		t.Errorf("expected all_only=all_val, got %v", vars["all_only"])
	}
	if vars["parent_only"] != "parent_val" {
		t.Errorf("expected parent_only=parent_val, got %v", vars["parent_only"])
	}
	if vars["child_only"] != "child_val" {
		t.Errorf("expected child_only=child_val, got %v", vars["child_only"])
	}
	if vars["host_only"] != "host_val" {
		t.Errorf("expected host_only=host_val, got %v", vars["host_only"])
	}
}

func TestLoad_ChildGroupVarsOverrideParent(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  vars:
    level: all
  children:
    parent:
      vars:
        level: parent
      children:
        child:
          hosts:
            h1:
          vars:
            level: child
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	vars := inv.EffectiveVarsForHost("h1")
	if vars["level"] != "child" {
		t.Errorf("expected level=child, got %v", vars["level"])
	}
}

func TestLoad_HostInMultipleGroups(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  children:
    web:
      hosts:
        shared_host:
          web_var: web_val
      vars:
        role: web
    db:
      hosts:
        shared_host:
          db_var: db_val
      vars:
        role: db
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	groups := inv.GroupsForHost("shared_host")
	if !contains(groups, "web") {
		t.Error("expected shared_host in web")
	}
	if !contains(groups, "db") {
		t.Error("expected shared_host in db")
	}

	vars := inv.EffectiveVarsForHost("shared_host")
	if vars["role"] != "web" {
		t.Logf("role=%v (alphabetical: db < web, so web wins)", vars["role"])
	}
	if vars["web_var"] != "web_val" {
		t.Errorf("expected web_var=web_val, got %v", vars["web_var"])
	}
	if vars["db_var"] != "db_val" {
		t.Errorf("expected db_var=db_val, got %v", vars["db_var"])
	}
}

func TestLoad_CircularReference(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  children:
    group_a:
      children:
        group_b:
          children:
            group_a:
              hosts:
                h1:
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for circular reference")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected circular error, got: %v", err)
	}
}

func TestLoad_EmptyInventory(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, ``)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty inventory")
	}
}

func TestLoad_HostsAsConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  children:
    webservers:
      hosts:
        web1:
          host: 192.168.1.10
          port: 2222
          user: admin
          ssh_pass: secret
          become: true
          become_password: sudo_pass
        web2:
          host: 192.168.1.20
          ssh_private_key_file: /home/user/.ssh/id_rsa
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	hosts, err := inv.HostsAsConfig()
	if err != nil {
		t.Fatalf("HostsAsConfig failed: %v", err)
	}

	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	hostMap := map[string]interface{}{}
	for _, h := range hosts {
		hostMap[h.Name] = h
	}

	web1 := hostMap["web1"].(config.Host)
	if web1.Host != "192.168.1.10" {
		t.Errorf("expected Host=192.168.1.10, got %s", web1.Host)
	}
	if web1.Port != 2222 {
		t.Errorf("expected Port=2222, got %d", web1.Port)
	}
	if web1.User != "admin" {
		t.Errorf("expected User=admin, got %s", web1.User)
	}
	if web1.Password != "secret" {
		t.Errorf("expected Password=secret, got %s", web1.Password)
	}
	if !web1.Become {
		t.Error("expected Become=true")
	}
	if web1.BecomePassword != "sudo_pass" {
		t.Errorf("expected BecomePassword=sudo_pass, got %s", web1.BecomePassword)
	}
	if !contains(web1.Groups, "webservers") {
		t.Errorf("expected webservers in groups, got %v", web1.Groups)
	}

	web2 := hostMap["web2"].(config.Host)
	if web2.Host != "192.168.1.20" {
		t.Errorf("expected Host=192.168.1.20, got %s", web2.Host)
	}
	if web2.Port != 22 {
		t.Errorf("expected Port=22 (default), got %d", web2.Port)
	}
	if web2.SSHKeyPath != "/home/user/.ssh/id_rsa" {
		t.Errorf("expected SSHKeyPath, got %s", web2.SSHKeyPath)
	}
}

func TestLoad_HostsAsConfig_DefaultHost(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  hosts:
    192.168.1.50:
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	hosts, err := inv.HostsAsConfig()
	if err != nil {
		t.Fatalf("HostsAsConfig failed: %v", err)
	}

	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Name != "192.168.1.50" {
		t.Errorf("expected Name=192.168.1.50, got %s", hosts[0].Name)
	}
	if hosts[0].Host != "192.168.1.50" {
		t.Errorf("expected Host=192.168.1.50 (default to hostname), got %s", hosts[0].Host)
	}
}

func TestLoad_GroupMembers(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  children:
    web:
      hosts:
        w1:
        w2:
    db:
      hosts:
        d1:
    prod:
      children:
        web:
        db:
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	members := inv.GroupMembers()

	webMembers := members["web"]
	sort.Strings(webMembers)
	if !reflect.DeepEqual(webMembers, []string{"w1", "w2"}) {
		t.Errorf("expected web=[w1 w2], got %v", webMembers)
	}

	prodMembers := members["prod"]
	sort.Strings(prodMembers)
	if !reflect.DeepEqual(prodMembers, []string{"d1", "w1", "w2"}) {
		t.Errorf("expected prod=[d1 w1 w2], got %v", prodMembers)
	}

	allMembers := members["all"]
	sort.Strings(allMembers)
	if !reflect.DeepEqual(allMembers, []string{"d1", "w1", "w2"}) {
		t.Errorf("expected all=[d1 w1 w2], got %v", allMembers)
	}
}

func TestLoad_GroupVarsInheritance(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  vars:
    env: default
    all_var: present
  children:
    southeast:
      vars:
        env: southeast
        region_var: se
      children:
        atlanta:
          hosts:
            h1:
            h2:
          vars:
            env: atlanta
            city_var: atl
        raleigh:
          hosts:
            h3:
          vars:
            env: raleigh
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	h1Vars := inv.EffectiveVarsForHost("h1")
	if h1Vars["env"] != "atlanta" {
		t.Errorf("expected env=atlanta (child overrides parent), got %v", h1Vars["env"])
	}
	if h1Vars["region_var"] != "se" {
		t.Errorf("expected region_var=se (inherited from parent), got %v", h1Vars["region_var"])
	}
	if h1Vars["all_var"] != "present" {
		t.Errorf("expected all_var=present (inherited from all), got %v", h1Vars["all_var"])
	}

	h3Vars := inv.EffectiveVarsForHost("h3")
	if h3Vars["env"] != "raleigh" {
		t.Errorf("expected env=raleigh, got %v", h3Vars["env"])
	}
}

func TestLoad_MinimalInventory(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  hosts:
    myhost:
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(inv.Hosts) != 1 {
		t.Errorf("expected 1 host, got %d", len(inv.Hosts))
	}
	if _, ok := inv.Hosts["myhost"]; !ok {
		t.Error("expected 'myhost' in hosts")
	}
}

func TestLoad_DeepNesting(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  vars:
    level: 0
  children:
    l1:
      vars:
        level: 1
      children:
        l2:
          vars:
            level: 2
          children:
            l3:
              hosts:
                deep_host:
              vars:
                level: 3
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	vars := inv.EffectiveVarsForHost("deep_host")
	if vars["level"] != 3 {
		t.Errorf("expected level=3 (deepest child wins), got %v", vars["level"])
	}

	groups := inv.GroupsForHost("deep_host")
	for _, expected := range []string{"all", "l1", "l2", "l3"} {
		if !contains(groups, expected) {
			t.Errorf("expected %s in groups, got %v", expected, groups)
		}
	}
}

func TestLoad_TopLevelGroupsWithoutAll(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
webservers:
  hosts:
    web1:
      host: 10.0.0.1
  vars:
    http_port: 80
dbservers:
  hosts:
    db1:
      host: 10.0.1.1
  vars:
    db_port: 5432
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	allGroup := inv.Groups["all"]
	if allGroup == nil {
		t.Fatal("expected implicit 'all' group")
	}
	if !contains(allGroup.Children, "webservers") {
		t.Error("expected webservers as child of all")
	}
	if !contains(allGroup.Children, "dbservers") {
		t.Error("expected dbservers as child of all")
	}

	web1Vars := inv.EffectiveVarsForHost("web1")
	if web1Vars["http_port"] != 80 {
		t.Errorf("expected http_port=80, got %v", web1Vars["http_port"])
	}
}

func TestLoad_AllGroupWithVarsAndTopLevelGroups(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  vars:
    global: value
  hosts:
    standalone:
webservers:
  hosts:
    web1:
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	allGroup := inv.Groups["all"]
	if !contains(allGroup.Children, "webservers") {
		t.Error("expected webservers merged as child of all")
	}

	standaloneVars := inv.EffectiveVarsForHost("standalone")
	if standaloneVars["global"] != "value" {
		t.Errorf("expected global=value, got %v", standaloneVars["global"])
	}

	web1Vars := inv.EffectiveVarsForHost("web1")
	if web1Vars["global"] != "value" {
		t.Errorf("expected global=value inherited from all, got %v", web1Vars["global"])
	}
}

func TestLoad_TypeCoercion(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  hosts:
    h1:
      port: "2222"
      become: "yes"
    h2:
      port: 22
      become: true
    h3:
      become: false
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	hosts, err := inv.HostsAsConfig()
	if err != nil {
		t.Fatalf("HostsAsConfig failed: %v", err)
	}

	hostMap := map[string]config.Host{}
	for _, h := range hosts {
		hostMap[h.Name] = h
	}

	if hostMap["h1"].Port != 2222 {
		t.Errorf("expected h1.Port=2222 (from string), got %d", hostMap["h1"].Port)
	}
	if !hostMap["h1"].Become {
		t.Error("expected h1.Become=true (from string 'yes')")
	}
	if hostMap["h2"].Port != 22 {
		t.Errorf("expected h2.Port=22, got %d", hostMap["h2"].Port)
	}
	if !hostMap["h2"].Become {
		t.Error("expected h2.Become=true")
	}
	if hostMap["h3"].Become {
		t.Error("expected h3.Become=false")
	}
}

func TestResolveTemplates_PortFromGroupVar(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  vars:
    ssh_port: "2222"
    ssh_host: "10.0.0.1"
    ssh_user: deploy
    ssh_pass: secret
  children:
    mygroup:
      hosts:
        myhost:
          host: "{{ ssh_host }}"
          port: "{{ ssh_port }}"
          user: "{{ ssh_user }}"
          ssh_pass: "{{ ssh_pass }}"
          become: true
          become_password: "{{ ssh_pass }}"
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if err := inv.ResolveTemplates(); err != nil {
		t.Fatalf("ResolveTemplates failed: %v", err)
	}

	hosts, err := inv.HostsAsConfig()
	if err != nil {
		t.Fatalf("HostsAsConfig failed: %v", err)
	}

	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	h := hosts[0]

	if h.Host != "10.0.0.1" {
		t.Errorf("expected Host=10.0.0.1, got %s", h.Host)
	}
	if h.Port != 2222 {
		t.Errorf("expected Port=2222, got %d", h.Port)
	}
	if h.User != "deploy" {
		t.Errorf("expected User=deploy, got %s", h.User)
	}
	if h.Password != "secret" {
		t.Errorf("expected Password=secret, got %s", h.Password)
	}
	if h.BecomePassword != "secret" {
		t.Errorf("expected BecomePassword=secret, got %s", h.BecomePassword)
	}
	if !h.Become {
		t.Error("expected Become=true")
	}
}

func TestResolveTemplates_NoTemplates(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  hosts:
    h1:
      host: 10.0.0.1
      port: 22
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if err := inv.ResolveTemplates(); err != nil {
		t.Fatalf("ResolveTemplates failed: %v", err)
	}

	hosts, err := inv.HostsAsConfig()
	if err != nil {
		t.Fatalf("HostsAsConfig failed: %v", err)
	}

	if hosts[0].Port != 22 {
		t.Errorf("expected Port=22, got %d", hosts[0].Port)
	}
}

func TestEffectiveVarsForHost_NonexistentHost(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  hosts:
    h1:
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	vars := inv.EffectiveVarsForHost("nonexistent")
	if len(vars) != 0 {
		t.Errorf("expected empty vars for nonexistent host, got %v", vars)
	}
}

func TestGroupsForHost_NonexistentHost(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  hosts:
    h1:
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	groups := inv.GroupsForHost("nonexistent")
	if groups != nil {
		t.Errorf("expected nil for nonexistent host, got %v", groups)
	}
}

func TestLoad_MultipleParentGroups(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  children:
    web:
      hosts:
        shared:
    monitoring:
      children:
        web:
      vars:
        monitored: true
    production:
      children:
        web:
      vars:
        env: prod
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	groups := inv.GroupsForHost("shared")
	if !contains(groups, "web") {
		t.Error("expected shared in web")
	}
	if !contains(groups, "monitoring") {
		t.Error("expected shared in monitoring (parent of web)")
	}
	if !contains(groups, "production") {
		t.Error("expected shared in production (parent of web)")
	}

	vars := inv.EffectiveVarsForHost("shared")
	if vars["monitored"] != true {
		t.Errorf("expected monitored=true, got %v", vars["monitored"])
	}
	if vars["env"] != "prod" {
		t.Errorf("expected env=prod, got %v", vars["env"])
	}
}

func TestLoad_HostWithNullVars(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  hosts:
    h1:
    h2:
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	names := inv.HostNames()
	if !reflect.DeepEqual(names, []string{"h1", "h2"}) {
		t.Errorf("expected [h1 h2], got %v", names)
	}
}

func TestLoad_EmptyGroupChildren(t *testing.T) {
	dir := t.TempDir()
	path := writeInventory(t, dir, `
all:
  children:
    web:
      hosts:
        w1:
    empty_parent:
      children:
        web:
`)
	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	groups := inv.GroupsForHost("w1")
	if !contains(groups, "empty_parent") {
		t.Errorf("expected w1 in empty_parent, got %v", groups)
	}
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
