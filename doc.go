// Package main is the executable entry point for CPE.
//
// main.go owns process-lifetime wiring, including installing the process-wide
// JSON logger and delegating command-line behavior to internal/cmd. logging.go
// owns the default log path, file-opening policy, process ID annotation, and
// context-aware handler composition.
package main
