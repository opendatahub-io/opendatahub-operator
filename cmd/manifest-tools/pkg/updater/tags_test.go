package updater

import "testing"

func TestParseTrackerURL(t *testing.T) {
	tests := []struct {
		url         string
		wantOwner   string
		wantRepo    string
		wantIssue   int
		wantErr     bool
	}{
		{
			url:       "https://github.com/opendatahub-io/odh-release-tracker/issues/42",
			wantOwner: "opendatahub-io",
			wantRepo:  "odh-release-tracker",
			wantIssue: 42,
		},
		{
			url:     "https://github.com/too/short",
			wantErr: true,
		},
		{
			url:     "https://github.com/org/repo/issues/notanumber",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			owner, repo, issue, err := parseTrackerURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tt.wantOwner || repo != tt.wantRepo || issue != tt.wantIssue {
				t.Errorf("got (%s, %s, %d), want (%s, %s, %d)", owner, repo, issue, tt.wantOwner, tt.wantRepo, tt.wantIssue)
			}
		})
	}
}

func TestImageNameToEnvVar(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"kube-auth-proxy", "RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE"},
		{"foo-image", "RELATED_IMAGE_ODH_FOO_IMAGE"},
		{"simple", "RELATED_IMAGE_ODH_SIMPLE_IMAGE"},
		{"already-has-image", "RELATED_IMAGE_ODH_ALREADY_HAS_IMAGE"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := imageNameToEnvVar(tt.input)
			if got != tt.expected {
				t.Errorf("imageNameToEnvVar(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"odh-dashboard", "odh-dashboard"},
		{"workbenches/notebook-controller", "workbenches-notebook-controller"},
		{"Some_Component", "some-component"},
		{"UPPER/CASE", "upper-case"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeName(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIndexOf(t *testing.T) {
	lines := []string{"line1", "#Release#", "line3", "#Images#", "line5"}

	if got := indexOf(lines, "#Release#"); got != 1 {
		t.Errorf("indexOf(#Release#) = %d, want 1", got)
	}
	if got := indexOf(lines, "#Images#"); got != 3 {
		t.Errorf("indexOf(#Images#) = %d, want 3", got)
	}
	if got := indexOf(lines, "missing"); got != -1 {
		t.Errorf("indexOf(missing) = %d, want -1", got)
	}
}
