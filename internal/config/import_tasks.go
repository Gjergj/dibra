package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const maxImportDepth = 50

func ExpandImportTasks(tasks []Task, baseDir string, renderPath func(string) (string, error)) ([]Task, error) {
	return expandImportTasks(tasks, baseDir, renderPath, nil, 0)
}

func expandImportTasks(tasks []Task, baseDir string, renderPath func(string) (string, error), stack []string, depth int) ([]Task, error) {
	if depth > maxImportDepth {
		return nil, fmt.Errorf("import_tasks: maximum nesting depth (%d) exceeded, import chain: %s", maxImportDepth, formatStack(stack))
	}

	var result []Task
	for _, task := range tasks {
		if task.ImportTasks == nil {
			result = append(result, task)
			continue
		}

		filePath := task.ImportTasks.File
		if filePath == "" {
			return nil, fmt.Errorf("import_tasks: file path is required (task: %q)", task.Name)
		}

		if renderPath != nil {
			rendered, err := renderPath(filePath)
			if err != nil {
				return nil, fmt.Errorf("import_tasks: failed to render file path %q: %w", filePath, err)
			}
			filePath = rendered
		}

		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(baseDir, filePath)
		}
		filePath = filepath.Clean(filePath)

		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return nil, fmt.Errorf("import_tasks: failed to resolve path %q: %w", filePath, err)
		}

		for _, s := range stack {
			if s == absPath {
				return nil, fmt.Errorf("import_tasks: circular import detected: %s -> %s", formatStack(stack), absPath)
			}
		}

		importedTasks, err := loadTasksFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("import_tasks: failed to load %q (task: %q): %w", filePath, task.Name, err)
		}

		if len(task.Vars) > 0 {
			for i := range importedTasks {
				if importedTasks[i].Vars == nil {
					importedTasks[i].Vars = make(map[string]interface{})
				}
				merged := make(map[string]interface{})
				for k, v := range task.Vars {
					merged[k] = v
				}
				for k, v := range importedTasks[i].Vars {
					merged[k] = v
				}
				importedTasks[i].Vars = merged
			}
		}

		if len(task.When) > 0 {
			for i := range importedTasks {
				importedTasks[i].When = mergeWhen(task.When, importedTasks[i].When)
			}
		}

		newStack := append(append([]string{}, stack...), absPath)
		newBaseDir := filepath.Dir(absPath)
		expanded, err := expandImportTasks(importedTasks, newBaseDir, renderPath, newStack, depth+1)
		if err != nil {
			return nil, err
		}

		result = append(result, expanded...)
	}

	return result, nil
}

func mergeWhen(parent When, child When) When {
	if len(parent) == 0 {
		return child
	}
	if len(child) == 0 {
		return parent
	}
	merged := make(When, 0, len(parent)+len(child))
	merged = append(merged, parent...)
	merged = append(merged, child...)
	return merged
}

func loadTasksFile(path string) ([]Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var tasks []Task
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&tasks); err != nil {
		return nil, fmt.Errorf("failed to parse tasks file %q: %w", path, err)
	}

	if tasks == nil {
		return nil, fmt.Errorf("tasks file %q is empty or contains no tasks", path)
	}

	return tasks, nil
}

func formatStack(stack []string) string {
	if len(stack) == 0 {
		return "(root)"
	}
	result := stack[0]
	for i := 1; i < len(stack); i++ {
		result += " -> " + stack[i]
	}
	return result
}
