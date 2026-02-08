package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandImportTasks_BasicExpansion(t *testing.T) {
	dir := t.TempDir()

	tasksContent := `- name: Imported task
  ping:
`
	writeFile(t, filepath.Join(dir, "tasks.yaml"), tasksContent)

	tasks := []Task{
		{Name: "Before"},
		{Name: "Import", ImportTasks: &ImportTasksParams{File: "tasks.yaml"}},
		{Name: "After"},
	}

	result, err := ExpandImportTasks(tasks, dir, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("Expected 3 tasks, got %d", len(result))
	}
	if result[0].Name != "Before" {
		t.Errorf("Expected 'Before', got %q", result[0].Name)
	}
	if result[1].Name != "Imported task" {
		t.Errorf("Expected 'Imported task', got %q", result[1].Name)
	}
	if result[2].Name != "After" {
		t.Errorf("Expected 'After', got %q", result[2].Name)
	}
}

func TestExpandImportTasks_MultipleTasksInFile(t *testing.T) {
	dir := t.TempDir()

	tasksContent := `- name: First imported
  ping:
- name: Second imported
  ping:
`
	writeFile(t, filepath.Join(dir, "tasks.yaml"), tasksContent)

	tasks := []Task{
		{Name: "Import", ImportTasks: &ImportTasksParams{File: "tasks.yaml"}},
	}

	result, err := ExpandImportTasks(tasks, dir, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(result))
	}
	if result[0].Name != "First imported" {
		t.Errorf("Expected 'First imported', got %q", result[0].Name)
	}
	if result[1].Name != "Second imported" {
		t.Errorf("Expected 'Second imported', got %q", result[1].Name)
	}
}

func TestExpandImportTasks_NestedImport(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	_ = os.MkdirAll(subDir, 0755)

	innerContent := `- name: Inner task
  ping:
`
	writeFile(t, filepath.Join(subDir, "inner.yaml"), innerContent)

	outerContent := `- name: Outer task
  ping:
- name: Import inner
  import_tasks: inner.yaml
`
	writeFile(t, filepath.Join(subDir, "outer.yaml"), outerContent)

	tasks := []Task{
		{Name: "Import outer", ImportTasks: &ImportTasksParams{File: "sub/outer.yaml"}},
	}

	result, err := ExpandImportTasks(tasks, dir, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(result))
	}
	if result[0].Name != "Outer task" {
		t.Errorf("Expected 'Outer task', got %q", result[0].Name)
	}
	if result[1].Name != "Inner task" {
		t.Errorf("Expected 'Inner task', got %q", result[1].Name)
	}
}

func TestExpandImportTasks_CircularDetection(t *testing.T) {
	dir := t.TempDir()

	aContent := `- name: Task A
  ping:
- name: Import B
  import_tasks: b.yaml
`
	writeFile(t, filepath.Join(dir, "a.yaml"), aContent)

	bContent := `- name: Task B
  ping:
- name: Import A
  import_tasks: a.yaml
`
	writeFile(t, filepath.Join(dir, "b.yaml"), bContent)

	tasks := []Task{
		{Name: "Import A", ImportTasks: &ImportTasksParams{File: "a.yaml"}},
	}

	_, err := ExpandImportTasks(tasks, dir, nil)
	if err == nil {
		t.Fatal("Expected circular import error, got nil")
	}
	if !strings.Contains(err.Error(), "circular import") {
		t.Errorf("Expected 'circular import' in error, got: %v", err)
	}
}

func TestExpandImportTasks_FileNotFound(t *testing.T) {
	dir := t.TempDir()

	tasks := []Task{
		{Name: "Import missing", ImportTasks: &ImportTasksParams{File: "nonexistent.yaml"}},
	}

	_, err := ExpandImportTasks(tasks, dir, nil)
	if err == nil {
		t.Fatal("Expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to load") {
		t.Errorf("Expected 'failed to load' in error, got: %v", err)
	}
}

func TestExpandImportTasks_EmptyFilePath(t *testing.T) {
	tasks := []Task{
		{Name: "Empty path", ImportTasks: &ImportTasksParams{File: ""}},
	}

	_, err := ExpandImportTasks(tasks, "/tmp", nil)
	if err == nil {
		t.Fatal("Expected error for empty file path, got nil")
	}
	if !strings.Contains(err.Error(), "file path is required") {
		t.Errorf("Expected 'file path is required' in error, got: %v", err)
	}
}

func TestExpandImportTasks_VarsInheritance(t *testing.T) {
	dir := t.TempDir()

	tasksContent := `- name: Task with own var
  vars:
    own_key: own_value
  ping:
- name: Task without vars
  ping:
`
	writeFile(t, filepath.Join(dir, "tasks.yaml"), tasksContent)

	tasks := []Task{
		{
			Name:        "Import with vars",
			ImportTasks: &ImportTasksParams{File: "tasks.yaml"},
			Vars: map[string]interface{}{
				"inherited_key": "inherited_value",
				"own_key":       "should_be_overridden",
			},
		},
	}

	result, err := ExpandImportTasks(tasks, dir, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(result))
	}

	// First task should have both inherited and own vars, with own taking precedence
	if result[0].Vars["inherited_key"] != "inherited_value" {
		t.Errorf("Expected inherited_key=inherited_value, got %v", result[0].Vars["inherited_key"])
	}
	if result[0].Vars["own_key"] != "own_value" {
		t.Errorf("Expected own_key=own_value (task's own wins), got %v", result[0].Vars["own_key"])
	}

	// Second task should have only inherited vars
	if result[1].Vars["inherited_key"] != "inherited_value" {
		t.Errorf("Expected inherited_key=inherited_value, got %v", result[1].Vars["inherited_key"])
	}
	if result[1].Vars["own_key"] != "should_be_overridden" {
		t.Errorf("Expected own_key=should_be_overridden, got %v", result[1].Vars["own_key"])
	}
}

func TestExpandImportTasks_RenderPath(t *testing.T) {
	dir := t.TempDir()

	tasksContent := `- name: Rendered task
  ping:
`
	writeFile(t, filepath.Join(dir, "actual.yaml"), tasksContent)

	tasks := []Task{
		{Name: "Import templated", ImportTasks: &ImportTasksParams{File: "{{ filename }}"}},
	}

	renderer := func(s string) (string, error) {
		return strings.ReplaceAll(s, "{{ filename }}", "actual.yaml"), nil
	}

	result, err := ExpandImportTasks(tasks, dir, renderer)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(result))
	}
	if result[0].Name != "Rendered task" {
		t.Errorf("Expected 'Rendered task', got %q", result[0].Name)
	}
}

func TestExpandImportTasks_AbsolutePath(t *testing.T) {
	dir := t.TempDir()

	tasksContent := `- name: Absolute path task
  ping:
`
	absPath := filepath.Join(dir, "tasks.yaml")
	writeFile(t, absPath, tasksContent)

	tasks := []Task{
		{Name: "Import absolute", ImportTasks: &ImportTasksParams{File: absPath}},
	}

	result, err := ExpandImportTasks(tasks, "/other/dir", nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(result))
	}
}

func TestExpandImportTasks_NoImports(t *testing.T) {
	tasks := []Task{
		{Name: "Regular task 1"},
		{Name: "Regular task 2"},
	}

	result, err := ExpandImportTasks(tasks, "/tmp", nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(result))
	}
}

func TestExpandImportTasks_SameFileTwice(t *testing.T) {
	dir := t.TempDir()

	tasksContent := `- name: Reusable task
  ping:
`
	writeFile(t, filepath.Join(dir, "reusable.yaml"), tasksContent)

	tasks := []Task{
		{Name: "Import 1", ImportTasks: &ImportTasksParams{File: "reusable.yaml"}},
		{Name: "Between"},
		{Name: "Import 2", ImportTasks: &ImportTasksParams{File: "reusable.yaml"}},
	}

	result, err := ExpandImportTasks(tasks, dir, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("Expected 3 tasks, got %d", len(result))
	}
	if result[0].Name != "Reusable task" {
		t.Errorf("Expected 'Reusable task', got %q", result[0].Name)
	}
	if result[1].Name != "Between" {
		t.Errorf("Expected 'Between', got %q", result[1].Name)
	}
	if result[2].Name != "Reusable task" {
		t.Errorf("Expected 'Reusable task', got %q", result[2].Name)
	}
}

func TestExpandImportTasks_DeeplyNested(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 5; i++ {
		subDir := dir
		for j := 0; j <= i; j++ {
			subDir = filepath.Join(subDir, "d")
		}
		_ = os.MkdirAll(subDir, 0755)
	}

	writeFile(t, filepath.Join(dir, "d/d/d/d/d/leaf.yaml"), `- name: Leaf task
  ping:
`)
	writeFile(t, filepath.Join(dir, "d/d/d/d/l4.yaml"), `- name: L4
  ping:
- name: Import leaf
  import_tasks: d/leaf.yaml
`)
	writeFile(t, filepath.Join(dir, "d/d/d/l3.yaml"), `- name: L3
  ping:
- name: Import L4
  import_tasks: d/l4.yaml
`)
	writeFile(t, filepath.Join(dir, "d/d/l2.yaml"), `- name: L2
  ping:
- name: Import L3
  import_tasks: d/l3.yaml
`)
	writeFile(t, filepath.Join(dir, "d/l1.yaml"), `- name: L1
  ping:
- name: Import L2
  import_tasks: d/l2.yaml
`)

	tasks := []Task{
		{Name: "Root", ImportTasks: &ImportTasksParams{File: "d/l1.yaml"}},
	}

	result, err := ExpandImportTasks(tasks, dir, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expectedNames := []string{"L1", "L2", "L3", "L4", "Leaf task"}
	if len(result) != len(expectedNames) {
		t.Fatalf("Expected %d tasks, got %d", len(expectedNames), len(result))
	}
	for i, name := range expectedNames {
		if result[i].Name != name {
			t.Errorf("Task %d: expected %q, got %q", i, name, result[i].Name)
		}
	}
}

func TestExpandImportTasks_RelativePathFromNestedFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "a/b"), 0755)

	writeFile(t, filepath.Join(dir, "a/b/sibling.yaml"), `- name: Sibling task
  ping:
`)
	writeFile(t, filepath.Join(dir, "a/parent.yaml"), `- name: Parent task
  ping:
- name: Import sibling
  import_tasks: b/sibling.yaml
`)

	tasks := []Task{
		{Name: "Import parent", ImportTasks: &ImportTasksParams{File: "a/parent.yaml"}},
	}

	result, err := ExpandImportTasks(tasks, dir, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(result))
	}
	if result[0].Name != "Parent task" {
		t.Errorf("Expected 'Parent task', got %q", result[0].Name)
	}
	if result[1].Name != "Sibling task" {
		t.Errorf("Expected 'Sibling task', got %q", result[1].Name)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write %s: %v", path, err)
	}
}
