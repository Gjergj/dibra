package vars

import "testing"

func TestRenderStringDynamicBracketExpression(t *testing.T) {
	context := map[string]interface{}{
		"inventory_hostname": "web1",
		"hostvars": map[string]interface{}{
			"web1": map[string]interface{}{
				"ansible_user_id": "root",
			},
		},
	}

	got, err := RenderString("user={{ hostvars[inventory_hostname].ansible_user_id }}", context)
	if err != nil {
		t.Fatalf("RenderString failed: %v", err)
	}
	if got != "user=root" {
		t.Fatalf("RenderString = %q, want user=root", got)
	}
}

func TestRenderStringQuotedBracketExpression(t *testing.T) {
	context := map[string]interface{}{
		"hostvars": map[string]interface{}{
			"web1": map[string]interface{}{
				"role": "frontend",
			},
		},
	}

	got, err := RenderString(`role={{ hostvars["web1"].role }}`, context)
	if err != nil {
		t.Fatalf("RenderString failed: %v", err)
	}
	if got != "role=frontend" {
		t.Fatalf("RenderString = %q, want role=frontend", got)
	}
}

func TestRenderStringUndefinedDynamicBracketExpression(t *testing.T) {
	context := map[string]interface{}{
		"hostvars": map[string]interface{}{},
	}

	if _, err := RenderString("{{ hostvars[inventory_hostname].role }}", context); err == nil {
		t.Fatal("expected undefined dynamic bracket expression to fail")
	}
}
