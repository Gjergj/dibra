package docker

import "testing"

func TestParseComposeJSONEventsComposeFive(t *testing.T) {
	output := []byte(`{"level":"warning","msg":"version is obsolete"}
{"id":"Image ghcr.io/example/app:tag","status":"Working","text":"Pulling"}
{"id":"layer-id","parent_id":"Image ghcr.io/example/app:tag","status":"Working"}
{"id":"layer-id","parent_id":"Image ghcr.io/example/app:tag","status":"Done","percent":100}
{"id":"Image ghcr.io/example/app:tag","status":"Done","text":"Pulled"}
{"id":"Container demo-web-1","status":"Working","text":"Creating"}
{"id":"Container demo-web-1","status":"Done","text":"Created"}
`)
	result := ParseComposeJSONEvents(output)
	if len(result.Warnings) != 1 || result.Warnings[0] != "version is obsolete" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if len(result.Events) != 6 {
		t.Fatalf("events = %#v", result.Events)
	}
	if result.Events[0] != (ComposeEvent{ResourceType: "image", ResourceID: "ghcr.io/example/app:tag", Status: "Pulling", Message: "Working"}) {
		t.Fatalf("first event = %#v", result.Events[0])
	}
	if result.Events[1].ResourceType != "image-layer" || result.Events[1].ResourceID != "ghcr.io/example/app:tag" {
		t.Fatalf("layer event = %#v", result.Events[1])
	}
	if !ComposeEventsChanged(result.Events) {
		t.Fatal("ComposeEventsChanged() = false")
	}
}

func TestParseComposeJSONEventsLegacyShapeStillEmittedByComposeFive(t *testing.T) {
	result := ParseComposeJSONEvents([]byte(`{"id":"demo-web","text":"Pulling"}
{"id":"abc","parent_id":"demo-web","text":"Downloading","status":"[=> ]"}
{"id":"demo-web","text":"Pulled"}
`))
	if len(result.Events) != 3 || result.Events[0].Status != "Pulling" || result.Events[1].ResourceType != "image-layer" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestValidateComposeVersion(t *testing.T) {
	for _, version := range []string{"5.4.0", "v5.4.0", "5.4.0-desktop.1"} {
		if err := ValidateComposeVersion(version); err != nil {
			t.Errorf("ValidateComposeVersion(%q) = %v", version, err)
		}
	}
	for _, version := range []string{"2.40.3", "5.3.9", "5.4.1", "5.5.0", "6.0.0", "dev"} {
		if err := ValidateComposeVersion(version); err == nil {
			t.Errorf("ValidateComposeVersion(%q) unexpectedly succeeded", version)
		}
	}
}
