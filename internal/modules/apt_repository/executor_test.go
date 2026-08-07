package apt_repository

import "testing"

func TestValidateSource(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr bool
	}{
		{
			name:   "binary repository",
			source: "deb https://example.com/debian stable main",
		},
		{
			name:   "source repository",
			source: "deb-src https://example.com/debian stable main contrib",
		},
		{
			name:   "single word options",
			source: "deb [arch=amd64] https://example.com/debian stable main",
		},
		{
			name:   "multi word options",
			source: "deb [arch=amd64 signed-by=/etc/apt/keyrings/example.gpg] https://example.com/debian stable main",
		},
		{
			name:   "path style suite",
			source: "deb https://example.com/debian stable/",
		},
		{
			name:    "empty source",
			source:  "   ",
			wantErr: true,
		},
		{
			name:    "unsupported source type",
			source:  "rpm https://example.com stable main",
			wantErr: true,
		},
		{
			name:    "missing suite",
			source:  "deb https://example.com/debian",
			wantErr: true,
		},
		{
			name:    "missing component",
			source:  "deb https://example.com/debian stable",
			wantErr: true,
		},
		{
			name:    "component on path style suite",
			source:  "deb https://example.com/debian stable/ main",
			wantErr: true,
		},
		{
			name:    "unterminated options",
			source:  "deb [arch=amd64 https://example.com/debian stable main",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSource(tt.source)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSource(%q) error = %v, wantErr %v", tt.source, err, tt.wantErr)
			}
		})
	}
}

func TestExecuteRejectsInvalidSourceBeforeStateHandling(t *testing.T) {
	resp := Execute(Request{Repo: "deb https://example.com/debian stable", State: "absent"})

	if !resp.Failed {
		t.Fatal("expected malformed source to fail")
	}
	if resp.Msg != "invalid repository string: source must include at least one component" {
		t.Fatalf("unexpected error: %s", resp.Msg)
	}
}
