package docker

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/moby/moby/client"
)

func TestFilterMapAcceptsStringListAndBooleanValues(t *testing.T) {
	var filters FilterMap
	if err := json.Unmarshal([]byte(`{"label":["foo=bar","bam=baz"],"dangling":true,"until":"24h"}`), &filters); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]string(filters["label"]), []string{"foo=bar", "bam=baz"}) {
		t.Fatalf("label = %#v", filters["label"])
	}
	if !reflect.DeepEqual([]string(filters["dangling"]), []string{"true"}) {
		t.Fatalf("dangling = %#v", filters["dangling"])
	}
	converted := filters.ToClientFilters()
	if !converted["until"]["24h"] || !converted["dangling"]["true"] || !converted["label"]["foo=bar"] {
		t.Fatalf("client filters = %#v", converted)
	}
	if _, ok := (client.Filters{})["missing"]; ok {
		t.Fatal("empty filters should stay empty")
	}
}

func TestStringifyAPIMapConvertsBooleans(t *testing.T) {
	result, err := StringifyAPIMap(map[string]any{"com.docker.network.bridge.enable_icc": false, "name": "net2"})
	if err != nil {
		t.Fatal(err)
	}
	if result["com.docker.network.bridge.enable_icc"] != "false" || result["name"] != "net2" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDecodeInspectionPreservesEngineFieldNames(t *testing.T) {
	decoded, err := DecodeInspection([]byte(`{"Id":"abc","Name":"web","EnableIPv6":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if decoded["Id"] != "abc" || decoded["Name"] != "web" {
		t.Fatalf("decoded = %#v", decoded)
	}
	if _, found := decoded["id"]; found {
		t.Fatalf("snake-case key leaked: %#v", decoded)
	}
}

func TestParseHumanBytesEmptyAndValid(t *testing.T) {
	if value, err := ParseHumanBytes(""); err != nil || value != 0 {
		t.Fatalf("empty = %d %v", value, err)
	}
	value, err := ParseHumanBytes("1MB")
	if err != nil || value != 1024*1024 {
		t.Fatalf("1MB = %d %v", value, err)
	}
	if _, err := ParseHumanBytes("bogus"); err == nil {
		t.Fatal("expected parse error")
	}
}
