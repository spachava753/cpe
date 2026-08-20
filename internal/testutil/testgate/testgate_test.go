package testgate

import "testing"

func TestEnvVar(t *testing.T) {
	tests := []struct {
		name string
		kind kind
		want string
	}{
		{name: "integration", kind: integration, want: envIntegrationTests},
		{name: "live", kind: live, want: envLiveTests},
		{name: "interactive", kind: interactive, want: envInteractiveTests},
		{name: "unknown", kind: kind("unknown"), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := envVar(tt.kind); got != tt.want {
				t.Fatalf("EnvVar(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestEnabled(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		want   bool
		kind   kind
		envVar string
	}{
		{name: "live disabled by default", kind: live, envVar: envLiveTests, want: false},
		{name: "live 1", kind: live, envVar: envLiveTests, value: "1", want: true},
		{name: "live true", kind: live, envVar: envLiveTests, value: "true", want: true},
		{name: "live yes", kind: live, envVar: envLiveTests, value: "yes", want: true},
		{name: "live on", kind: live, envVar: envLiveTests, value: "on", want: true},
		{name: "interactive false", kind: interactive, envVar: envInteractiveTests, value: "false", want: false},
		{name: "integration uppercase true", kind: integration, envVar: envIntegrationTests, value: "TRUE", want: true},
		{name: "unknown kind", kind: kind("unknown"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVar != "" {
				t.Setenv(tt.envVar, tt.value)
			}
			if got := enabled(tt.kind); got != tt.want {
				t.Fatalf("Enabled(%q) = %t, want %t", tt.kind, got, tt.want)
			}
		})
	}
}

func TestMissingEnv(t *testing.T) {
	t.Setenv("TESTGATE_PRESENT", "value")
	t.Setenv("TESTGATE_EMPTY", "")

	got := missingEnv("TESTGATE_PRESENT", "TESTGATE_EMPTY", "TESTGATE_MISSING", "")
	want := []string{"TESTGATE_EMPTY", "TESTGATE_MISSING"}
	if len(got) != len(want) {
		t.Fatalf("missingEnv length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("missingEnv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDescribe(t *testing.T) {
	t.Setenv(envLiveTests, "1")
	got := describe(live)
	want := "live tests enabled=true via CPE_RUN_LIVE_TESTS"
	if got != want {
		t.Fatalf("Describe(Live) = %q, want %q", got, want)
	}
}
