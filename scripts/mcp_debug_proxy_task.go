package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/goyek/goyek/v2"
)

// MCPDebugProxy starts an MCP stdio debug proxy
var MCPDebugProxy = goyek.Define(goyek.Task{
	Name:  "mcp-debug-proxy",
	Usage: "MCP stdio debug proxy. Use -log=FILE -cmd='command args'",
	Action: func(a *goyek.A) {
		logPath := GetLogFile()
		if logPath == "" {
			a.Fatal("Usage: go run ./scripts -log=<file> -cmd='<command>' mcp-debug-proxy")
		}

		mcpCmd := GetMCPCmd()
		if mcpCmd == "" {
			a.Fatal("Usage: go run ./scripts -log=<file> -cmd='<command>' mcp-debug-proxy")
		}

		cmdArgs := strings.Fields(mcpCmd)
		if len(cmdArgs) == 0 {
			a.Fatal("-cmd cannot be empty")
		}

		logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			a.Fatalf("Failed to open log file: %v", err)
		}
		defer logFile.Close()

		fmt.Fprintf(logFile, "=== MCP Debug Proxy Started at %s ===\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(logFile, "Command: %s\n", strings.Join(cmdArgs, " "))
		fmt.Fprintf(logFile, "=========================================\n\n")

		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

		stdinPipe, err := cmd.StdinPipe()
		if err != nil {
			a.Fatalf("Failed to get stdin pipe: %v", err)
		}

		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			a.Fatalf("Failed to get stdout pipe: %v", err)
		}

		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			a.Fatalf("Failed to start command: %v", err)
		}

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			sig := <-sigChan
			fmt.Fprintf(logFile, "[%s] Received signal: %v, propagating to child...\n", mcpTimestamp(), sig)
			logFile.Sync()
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		}()

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			defer stdinPipe.Close()
			reader := bufio.NewReader(os.Stdin)
			for {
				line, err := reader.ReadBytes('\n')
				if err != nil {
					if err != io.EOF {
						fmt.Fprintf(logFile, "[%s] STDIN ERROR: %v\n", mcpTimestamp(), err)
					}
					return
				}
				fmt.Fprintf(logFile, "[%s] --> %s", mcpTimestamp(), line)
				logFile.Sync()
				stdinPipe.Write(line)
			}
		}()

		go func() {
			defer wg.Done()
			reader := bufio.NewReader(stdoutPipe)
			for {
				line, err := reader.ReadBytes('\n')
				if err != nil {
					if err != io.EOF {
						fmt.Fprintf(logFile, "[%s] STDOUT ERROR: %v\n", mcpTimestamp(), err)
					}
					return
				}
				fmt.Fprintf(logFile, "[%s] <-- %s", mcpTimestamp(), line)
				logFile.Sync()
				os.Stdout.Write(line)
			}
		}()

		err = cmd.Wait()
		fmt.Fprintf(logFile, "[%s] Child process exited: %v\n", mcpTimestamp(), err)
		logFile.Sync()

		wg.Wait()

		fmt.Fprintf(logFile, "[%s] === MCP Debug Proxy Shutdown ===\n", mcpTimestamp())
	},
})

func mcpTimestamp() string {
	return time.Now().Format("15:04:05.000")
}
