// Package main in the build directory defines Goyek-powered developer tasks.
// The lint task combines golangci-lint, modernize, unused-export analysis, and
// deadcode. Invoke tasks with go run ./build <task>; no arguments lists tasks.
// Unused-export autofixes decline lexical captures and embedded type renames;
// embedded names also determine field promotion and may affect serialization.
package main
