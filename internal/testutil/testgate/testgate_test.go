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

func TestTruthyEnv(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		want   bool
		envVar string
	}{
		{name: "disabled by default", envVar: envLiveTests},
		{name: "1", envVar: envLiveTests, value: "1", want: true},
		{name: "true", envVar: envLiveTests, value: "true", want: true},
		{name: "yes", envVar: envLiveTests, value: "yes", want: true},
		{name: "on", envVar: envLiveTests, value: "on", want: true},
		{name: "false", envVar: envInteractiveTests, value: "false"},
		{name: "uppercase true", envVar: envIntegrationTests, value: "TRUE", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envVar, tt.value)
			if got := truthyEnv(tt.envVar); got != tt.want {
				t.Fatalf("truthyEnv(%q) = %t, want %t", tt.envVar, got, tt.want)
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
