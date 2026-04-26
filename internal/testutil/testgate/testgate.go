package testgate

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Kind identifies a category of opt-in test.
type Kind string

const (
	// Integration identifies tests that require real local services, binaries,
	// or multi-process orchestration.
	Integration Kind = "integration"
	// Live identifies tests that call real external APIs or accounts.
	Live Kind = "live"
	// Interactive identifies tests that require a browser, manual approval, or
	// another human-in-the-loop step.
	Interactive Kind = "interactive"
)

const (
	// EnvIntegrationTests enables tests gated by Integration.
	EnvIntegrationTests = "CPE_RUN_INTEGRATION_TESTS"
	// EnvLiveTests enables tests gated by Live.
	EnvLiveTests = "CPE_RUN_LIVE_TESTS"
	// EnvInteractiveTests enables tests gated by Interactive.
	EnvInteractiveTests = "CPE_RUN_INTERACTIVE_TESTS"
)

// EnvVar returns the environment variable that enables the given test kind.
// It returns the empty string for unknown kinds.
func EnvVar(kind Kind) string {
	switch kind {
	case Integration:
		return EnvIntegrationTests
	case Live:
		return EnvLiveTests
	case Interactive:
		return EnvInteractiveTests
	default:
		return ""
	}
}

// Enabled reports whether the given opt-in test kind is enabled by the
// corresponding environment variable.
func Enabled(kind Kind) bool {
	envVar := EnvVar(kind)
	if envVar == "" {
		return false
	}
	return truthyEnv(envVar)
}

// Require skips the current test unless the given opt-in test kind is enabled.
func Require(t testing.TB, kind Kind) {
	t.Helper()
	if Enabled(kind) {
		return
	}
	envVar := EnvVar(kind)
	if envVar == "" {
		t.Fatalf("unknown test gate kind %q", kind)
	}
	t.Skipf("skipping %s test; set %s=1 to enable", kind, envVar)
}

// RequireLive skips the current test unless live tests are enabled.
func RequireLive(t testing.TB) {
	Require(t, Live)
}

// RequireEnv skips the current test when any required environment variable is
// unset or empty.
func RequireEnv(t testing.TB, vars ...string) {
	t.Helper()
	missing := missingEnv(vars...)
	if len(missing) == 0 {
		return
	}
	t.Skipf("skipping test; required environment variables are not set: %s", strings.Join(missing, ", "))
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

// Describe returns a human-readable summary of whether the given test kind is
// enabled and which environment variable controls it.
func Describe(kind Kind) string {
	envVar := EnvVar(kind)
	if envVar == "" {
		return fmt.Sprintf("unknown test gate kind %q", kind)
	}
	return fmt.Sprintf("%s tests enabled=%t via %s", kind, Enabled(kind), envVar)
}
