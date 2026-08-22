package controller

import (
	"fmt"

	"github.com/gjergjiramku/dibra/internal/config"
	"github.com/gjergjiramku/dibra/internal/template"
	"github.com/gjergjiramku/dibra/internal/vars"
)

type handlerDefinition struct {
	task     config.Task
	name     string
	position int
}

type handlerIndex struct {
	definitions []handlerDefinition
	byName      map[string]int
	byTopic     map[string][]int
}

func buildHandlerIndex(tasks []config.Task, renderName func(config.Task) (string, error)) (handlerIndex, error) {
	renderedNames := make([]string, len(tasks))
	lastByName := make(map[string]int, len(tasks))
	for position, task := range tasks {
		if task.Name == "" {
			return handlerIndex{}, fmt.Errorf("handler at position %d must have a name", position+1)
		}
		name, err := renderName(task)
		if err != nil {
			return handlerIndex{}, fmt.Errorf("render handler name %q: %w", task.Name, err)
		}
		renderedNames[position] = name
		lastByName[name] = position
	}

	index := handlerIndex{
		byName:  make(map[string]int, len(lastByName)),
		byTopic: make(map[string][]int),
	}
	for position, task := range tasks {
		name := renderedNames[position]
		if lastByName[name] != position {
			continue
		}
		definitionIndex := len(index.definitions)
		index.definitions = append(index.definitions, handlerDefinition{
			task:     task,
			name:     name,
			position: position,
		})
		index.byName[name] = definitionIndex
		for _, topic := range task.Listen {
			index.byTopic[topic] = append(index.byTopic[topic], definitionIndex)
		}
	}
	return index, nil
}

func (i handlerIndex) queue(target string, pending map[int]struct{}) bool {
	resolved := false
	if definitionIndex, ok := i.byName[target]; ok {
		pending[definitionIndex] = struct{}{}
		resolved = true
	}
	for _, definitionIndex := range i.byTopic[target] {
		pending[definitionIndex] = struct{}{}
		resolved = true
	}
	return resolved
}

func applyChangedWhen(task config.Task, result map[string]interface{}, context map[string]interface{}) error {
	if len(task.ChangedWhen) == 0 || result == nil {
		return nil
	}
	if skipped, _ := result["skipped"].(bool); skipped {
		return nil
	}
	evaluationContext := context
	if task.Register != "" {
		evaluationContext = make(map[string]interface{}, len(context)+1)
		for key, value := range context {
			evaluationContext[key] = value
		}
		evaluationContext[task.Register] = result
	}
	changed, err := template.EvaluateWhen(task.ChangedWhen, evaluationContext)
	if err != nil {
		return fmt.Errorf("changed_when: %w", err)
	}
	result["changed"] = changed
	return nil
}

func queueTaskNotifications(
	task config.Task,
	result map[string]interface{},
	context map[string]interface{},
	index handlerIndex,
	pending map[int]struct{},
	warn func(string, ...interface{}),
) error {
	if len(task.Notify) == 0 || result == nil {
		return nil
	}
	if failed, _ := result["failed"].(bool); failed {
		return nil
	}
	if skipped, _ := result["skipped"].(bool); skipped {
		return nil
	}
	if changed, _ := result["changed"].(bool); !changed {
		return nil
	}
	for _, rawTarget := range task.Notify {
		target, err := vars.RenderString(rawTarget, context)
		if err != nil {
			return fmt.Errorf("render notify target %q: %w", rawTarget, err)
		}
		if !index.queue(target, pending) && warn != nil {
			warn("    ⚠ requested handler %q was not found\n", target)
		}
	}
	return nil
}

func queueNotificationsForChangedLoop(
	task config.Task,
	iterationContexts []map[string]interface{},
	loopChanged bool,
	loopFailed bool,
	index handlerIndex,
	pending map[int]struct{},
	warn func(string, ...interface{}),
) error {
	if !loopChanged || loopFailed || len(task.Notify) == 0 {
		return nil
	}
	cleared := task
	cleared.Loop = nil
	cleared.WithItems = nil
	cleared.WithList = nil
	cleared.WithDict = nil
	cleared.WithSequence = nil
	cleared.LoopControl = nil
	changed := map[string]interface{}{"changed": true, "failed": false}
	for _, iterationContext := range iterationContexts {
		if err := queueTaskNotifications(cleared, changed, iterationContext, index, pending, warn); err != nil {
			return err
		}
	}
	return nil
}
