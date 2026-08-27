package testgate

import (
	"os"
	"strings"
	"testing"
)

// kind identifies a category of opt-in test.
type kind string

const (
	// integration identifies tests that require real local services, binaries,
	// or multi-process orchestration.
	integration kind = "integration"
	// live identifies tests that call real external APIs or accounts.
	live kind = "live"
	// interactive identifies tests that require a browser, manual approval, or
	// another human-in-the-loop step.
	interactive kind = "interactive"
)

const (
	// envIntegrationTests enables tests gated by integration.
	envIntegrationTests = "CPE_RUN_INTEGRATION_TESTS"
	// envLiveTests enables tests gated by live.
	envLiveTests = "CPE_RUN_LIVE_TESTS"
	// envInteractiveTests enables tests gated by interactive.
	envInteractiveTests = "CPE_RUN_INTERACTIVE_TESTS"
)

// envVar returns the environment variable that enables the given test kind.
// It returns the empty string for unknown kinds.
func envVar(kind kind) string {
	switch kind {
	case integration:
		return envIntegrationTests
	case live:
		return envLiveTests
	case interactive:
		return envInteractiveTests
	default:
		return ""
	}
}

// require skips the current test unless the given opt-in test kind is enabled.
func require(t testing.TB, kind kind) {
	t.Helper()
	envVar := envVar(kind)
	if envVar == "" {
		t.Fatalf("unknown test gate kind %q", kind)
	}
	if truthyEnv(envVar) {
		return
	}
	t.Skipf("skipping %s test; set %s=1 to enable", kind, envVar)
}

// RequireLive skips the current test unless live tests are enabled.
func RequireLive(t testing.TB) {
	require(t, live)
}

func missingEnv(vars ...string) []string {
	missing := make([]string, 0, len(vars))
	for _, envVar := range vars {
		if strings.TrimSpace(envVar) == "" {
			continue
		}
		if strings.TrimSpace(os.Getenv(envVar)) == "" {
			missing = append(missing, envVar)
		}
	}
	return missing
}

func truthyEnv(envVar string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envVar))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
