package testgate

import "testing"

func TestEnvVar(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		want string
	}{
		{name: "integration", kind: Integration, want: EnvIntegrationTests},
		{name: "live", kind: Live, want: EnvLiveTests},
		{name: "interactive", kind: Interactive, want: EnvInteractiveTests},
		{name: "unknown", kind: Kind("unknown"), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EnvVar(tt.kind); got != tt.want {
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
		kind   Kind
		envVar string
	}{
		{name: "live disabled by default", kind: Live, envVar: EnvLiveTests, want: false},
		{name: "live 1", kind: Live, envVar: EnvLiveTests, value: "1", want: true},
		{name: "live true", kind: Live, envVar: EnvLiveTests, value: "true", want: true},
		{name: "live yes", kind: Live, envVar: EnvLiveTests, value: "yes", want: true},
		{name: "live on", kind: Live, envVar: EnvLiveTests, value: "on", want: true},
		{name: "interactive false", kind: Interactive, envVar: EnvInteractiveTests, value: "false", want: false},
		{name: "integration uppercase true", kind: Integration, envVar: EnvIntegrationTests, value: "TRUE", want: true},
		{name: "unknown kind", kind: Kind("unknown"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVar != "" {
				t.Setenv(tt.envVar, tt.value)
			}
			if got := Enabled(tt.kind); got != tt.want {
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
	t.Setenv(EnvLiveTests, "1")
	got := Describe(Live)
	want := "live tests enabled=true via CPE_RUN_LIVE_TESTS"
	if got != want {
		t.Fatalf("Describe(Live) = %q, want %q", got, want)
	}
}
