// Package main in the build directory defines Goyek-powered developer tasks
// used for linting, schema generation, and debugging proxies. The lint task
// combines standard analyzers with repository-wide checks for unreachable code
// and package-level declarations that can be safely unexported.
//
// Tasks are invoked with `go run ./build <task>` and replace ad-hoc shell
// scripts with typed Go implementations.
package main
