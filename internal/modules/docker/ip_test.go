package docker

import "testing"

func TestNormalizeIPAddress(t *testing.T) {
	tests := map[string]string{
		"192.0.2.1":                         "192.0.2.1",
		"2001:0db8:0000:0000:0000:0000:0:1": "2001:db8::1",
		"not-an-address":                    "not-an-address",
	}
	for input, want := range tests {
		if got := NormalizeIPAddress(input); got != want {
			t.Errorf("NormalizeIPAddress(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeIPNetwork(t *testing.T) {
	tests := map[string]string{
		"192.0.2.0/24":        "192.0.2.0/24",
		"2001:0db8:0000::/64": "2001:db8::/64",
		"192.0.2.1/24":        "192.0.2.1/24",
		"not-a-network":       "not-a-network",
	}
	for input, want := range tests {
		if got := NormalizeIPNetwork(input); got != want {
			t.Errorf("NormalizeIPNetwork(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeEndpointAddress(t *testing.T) {
	if got := NormalizeEndpointAddress("2001:0db8::1/64"); got != "2001:db8::1" {
		t.Fatalf("NormalizeEndpointAddress() = %q", got)
	}
}
