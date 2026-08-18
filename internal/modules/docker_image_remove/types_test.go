package docker_image_remove

import (
	"encoding/json"
	"testing"
)

func TestFailedResponseOmitsSuccessFields(t *testing.T) {
	data, err := json.Marshal(failedResponse("boom"))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"actions", "image", "deleted", "untagged", "diff"} {
		if _, found := result[field]; found {
			t.Fatalf("failed response contains %q: %s", field, data)
		}
	}
}
