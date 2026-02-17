package main

import (
	"flag"

	"github.com/goyek/goyek/v2"
)

// Flags for debug-proxy task
var (
	targetURL = flag.String("target", "", "Target URL to proxy (for debug-proxy)")
	port      = flag.String("port", "8080", "Port to listen on (for debug-proxy)")
)

// Flags for mcp-debug-proxy task
var (
	logFile = flag.String("log", "", "Log file path (for mcp-debug-proxy)")
	mcpCmd  = flag.String("cmd", "", "MCP command to run (for mcp-debug-proxy)")
)

// Flags for lint task
var (
	lintFix     = flag.Bool("lint-fix", false, "Auto-fix linting issues")
	lintVerbose = flag.Bool("lint-verbose", false, "Verbose linting output")
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		args = []string{"list"}
	}
	goyek.Main(args)
}
