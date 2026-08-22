package controller

import (
	"reflect"
	"testing"

	"github.com/gjergjiramku/dibra/internal/config"
)

func TestHandlerIndexUsesDefinitionOrderTopicsAndLastDuplicate(t *testing.T) {
	tasks := []config.Task{
		{Name: "first", Listen: config.StringList{"restart web"}},
		{Name: "duplicate", Listen: config.StringList{"obsolete topic"}},
		{Name: "second", Listen: config.StringList{"restart web"}},
		{Name: "duplicate", Listen: config.StringList{"current topic"}},
	}
	index, err := buildHandlerIndex(tasks, func(task config.Task) (string, error) {
		return task.Name, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(index.definitions))
	for position, definition := range index.definitions {
		names[position] = definition.name
	}
	if !reflect.DeepEqual(names, []string{"first", "second", "duplicate"}) {
		t.Fatalf("definition order = %#v", names)
	}

	pending := map[int]struct{}{}
	if !index.queue("duplicate", pending) || !index.queue("restart web", pending) {
		t.Fatal("known handlers did not resolve")
	}
	if index.queue("obsolete topic", pending) {
		t.Fatal("shadowed duplicate handler topic resolved")
	}
	if len(pending) != 3 {
		t.Fatalf("pending = %#v", pending)
	}
}

func TestHandlerIndexRendersNamesAndRejectsUnnamedHandlers(t *testing.T) {
	index, err := buildHandlerIndex([]config.Task{{Name: "restart {{ service }}"}}, func(task config.Task) (string, error) {
		return "restart caddy", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := index.byName["restart caddy"]; !ok {
		t.Fatalf("rendered name index = %#v", index.byName)
	}
	if _, err := buildHandlerIndex([]config.Task{{}}, func(task config.Task) (string, error) {
		return task.Name, nil
	}); err == nil {
		t.Fatal("expected unnamed handler error")
	}
}

func TestApplyChangedWhenControlsNotifications(t *testing.T) {
	index, err := buildHandlerIndex([]config.Task{{Name: "restart"}}, func(task config.Task) (string, error) {
		return task.Name, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	task := config.Task{
		ChangedWhen: config.When{false},
		Notify:      config.StringList{"restart"},
	}
	result := map[string]interface{}{"changed": true, "failed": false}
	if err := applyChangedWhen(task, result, map[string]interface{}{}); err != nil {
		t.Fatal(err)
	}
	pending := map[int]struct{}{}
	if err := queueTaskNotifications(task, result, map[string]interface{}{}, index, pending, nil); err != nil {
		t.Fatal(err)
	}
	if result["changed"] != false || len(pending) != 0 {
		t.Fatalf("changed_when false result=%#v pending=%#v", result, pending)
	}
}

func TestApplyChangedWhenCanReferenceCurrentRegisteredResult(t *testing.T) {
	task := config.Task{
		Register:    "command_result",
		ChangedWhen: config.When{"(command_result.rc | int) != 0"},
	}
	result := map[string]interface{}{"changed": true, "failed": false, "rc": float64(0)}
	if err := applyChangedWhen(task, result, map[string]interface{}{}); err != nil {
		t.Fatal(err)
	}
	if result["changed"] != false {
		t.Fatalf("result = %#v", result)
	}
}

func TestQueueTaskNotificationsRendersAndDeduplicatesTargets(t *testing.T) {
	index, err := buildHandlerIndex([]config.Task{{Name: "restart caddy"}}, func(task config.Task) (string, error) {
		return task.Name, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	task := config.Task{Notify: config.StringList{"restart {{ service }}", "restart caddy"}}
	result := map[string]interface{}{"changed": true, "failed": false}
	pending := map[int]struct{}{}
	if err := queueTaskNotifications(task, result, map[string]interface{}{"service": "caddy"}, index, pending, nil); err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %#v", pending)
	}

	result["failed"] = true
	pending = map[int]struct{}{}
	if err := queueTaskNotifications(task, result, map[string]interface{}{"service": "caddy"}, index, pending, nil); err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("failed task notified handlers: %#v", pending)
	}
}

func TestQueueNotificationsForChangedLoopNotifiesEveryItem(t *testing.T) {
	index, err := buildHandlerIndex([]config.Task{
		{Name: "Restart memcached"},
		{Name: "Restart apache"},
	}, func(task config.Task) (string, error) {
		return task.Name, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	task := config.Task{Notify: config.StringList{"Restart {{ item }}"}}
	pending := map[int]struct{}{}
	if err := queueNotificationsForChangedLoop(task, []map[string]interface{}{
		{"item": "memcached"},
		{"item": "apache"},
	}, true, false, index, pending, nil); err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending = %#v, want both loop handlers", pending)
	}

	pending = map[int]struct{}{}
	if err := queueNotificationsForChangedLoop(task, []map[string]interface{}{
		{"item": "memcached"},
		{"item": "apache"},
	}, false, false, index, pending, nil); err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("unchanged loop notified handlers: %#v", pending)
	}
}
