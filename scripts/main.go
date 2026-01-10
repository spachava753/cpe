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

func main() {
	flag.Parse()
	goyek.Main(flag.Args())
}

// GetTargetURL returns the target URL flag value
func GetTargetURL() string { return *targetURL }

// GetPort returns the port flag value
func GetPort() string { return *port }

// GetLogFile returns the log file flag value
func GetLogFile() string { return *logFile }

// GetMCPCmd returns the MCP command flag value
func GetMCPCmd() string { return *mcpCmd }
