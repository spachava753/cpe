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

	func TestLocalProcess(t *testing.T) {
	    testgate.RequireIntegration(t)
	    // ... exercise local process orchestration ...
	}

Use RequireLive for tests that consume credentials and call a real provider.

Prefer fakes, `httptest`, and local fixtures for ordinary tests. Reach for this
package only when the real integration itself is what needs verification.
*/
package testgate
