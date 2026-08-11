package execution

import (
	"reflect"
	"testing"
)

func TestNormalizeResultPreservesModuleFieldsAndAddsCommonFields(t *testing.T) {
	result, err := NormalizeResult(map[string]any{
		"changed":  true,
		"image_id": "sha256:123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["changed"] != true || result["failed"] != false || result["msg"] != "" {
		t.Fatalf("common result fields = %#v", result)
	}
	if result["image_id"] != "sha256:123" {
		t.Fatalf("module return field was not preserved: %#v", result)
	}
}

func TestNormalizeResultConvertsLegacyFieldDiff(t *testing.T) {
	result, err := NormalizeResult(map[string]any{
		"diff": map[string]any{
			"image":    map[string]any{"before": "nginx:1", "after": "nginx:2"},
			"replicas": map[string]any{"before": float64(1), "after": float64(3)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"before": map[string]any{"image": "nginx:1", "replicas": float64(1)},
		"after":  map[string]any{"image": "nginx:2", "replicas": float64(3)},
	}
	if !reflect.DeepEqual(result["diff"], want) {
		t.Fatalf("diff = %#v, want %#v", result["diff"], want)
	}
}

func TestNormalizeResultConvertsLegacySwarmChangedFieldsDiff(t *testing.T) {
	result, err := NormalizeResult(map[string]any{
		"changed": true,
		"diff": map[string]any{
			"changed_fields": []string{"replicas", "image"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"before": map[string]any{"changed_fields": []string{}},
		"after":  map[string]any{"changed_fields": []string{"replicas", "image"}},
	}
	if !reflect.DeepEqual(result["diff"], want) {
		t.Fatalf("diff = %#v, want %#v", result["diff"], want)
	}
}

func TestNormalizeResultRejectsInvalidCommonFields(t *testing.T) {
	for name, input := range map[string]map[string]any{
		"changed": {"changed": "yes"},
		"failed":  {"failed": 1},
		"msg":     {"msg": []string{"bad"}},
		"diff":    {"diff": map[string]any{"unexpected": []string{"image"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeResult(input); err == nil {
				t.Fatalf("NormalizeResult(%#v) succeeded", input)
			}
		})
	}
}
