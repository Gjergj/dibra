package docker_swarm_service

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSizeAndDurationJSONRoundTrip(t *testing.T) {
	size := SizeValue{Bytes: 67108864}
	raw, err := json.Marshal(size)
	if err != nil || string(raw) != "67108864" {
		t.Fatalf("size marshal = %s %v", raw, err)
	}
	var decoded SizeValue
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Bytes != size.Bytes {
		t.Fatalf("size unmarshal = %#v %v", decoded, err)
	}
	if err := json.Unmarshal([]byte(`{"Bytes": 64}`), &decoded); err != nil || decoded.Bytes != 64 {
		t.Fatalf("size wrapped = %#v %v", decoded, err)
	}

	duration := DurationValue{Nanoseconds: 2003004000}
	raw, err = json.Marshal(duration)
	if err != nil || string(raw) != "2003004000" {
		t.Fatalf("duration marshal = %s %v", raw, err)
	}
	var decodedDuration DurationValue
	if err := json.Unmarshal(raw, &decodedDuration); err != nil || decodedDuration.Nanoseconds != duration.Nanoseconds {
		t.Fatalf("duration unmarshal = %#v %v", decodedDuration, err)
	}
	if err := json.Unmarshal([]byte(`{"Nanoseconds": 5}`), &decodedDuration); err != nil || decodedDuration.Nanoseconds != 5 {
		t.Fatalf("duration wrapped = %#v %v", decodedDuration, err)
	}
	if err := json.Unmarshal([]byte(`"5s"`), &decodedDuration); err != nil || decodedDuration.Nanoseconds != int64(5*time.Second) {
		t.Fatalf("duration string = %#v %v", decodedDuration, err)
	}
}
