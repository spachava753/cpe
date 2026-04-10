/*
Package testgate provides a shared opt-in pattern for tests that should not run
in the default `go test ./...` flow.

Use this package for tests that are more expensive or environment-dependent than
normal unit tests:
  - integration tests that depend on real local services, binaries, or process
    orchestration;
  - live tests that talk to real external APIs or accounts;
  - interactive tests that require a human, a browser, or other manual steps.

Environment gates:
  - `CPE_RUN_INTEGRATION_TESTS=1` enables integration tests;
  - `CPE_RUN_LIVE_TESTS=1` enables live tests;
  - `CPE_RUN_INTERACTIVE_TESTS=1` enables interactive tests.

Typical usage:

	func TestLiveOpenAIFlow(t *testing.T) {
		testgate.RequireLive(t)
		testgate.RequireInteractive(t)
		testgate.RequireEnv(t, "OPENAI_API_KEY")
		// ... run the live browser-backed flow ...
	}

If a test only needs to branch on enablement instead of skipping immediately,
use Enabled or the category-specific helpers such as LiveEnabled.

Prefer fakes, `httptest`, and local fixtures for ordinary tests. Reach for this
package only when the real integration itself is what needs verification.
*/
package testgate
