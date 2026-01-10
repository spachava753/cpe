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
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <log_file> <command> [args...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s debug.log go run main.go mcp serve\n", os.Args[0])
		os.Exit(1)
	}

	logPath := os.Args[1]
	cmdArgs := os.Args[2:]

	// Open log file in append mode, create if not exists
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open log file: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()

	fmt.Fprintf(logFile, "=== MCP Debug Proxy Started at %s ===\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(logFile, "Command: %s\n", strings.Join(cmdArgs, " "))
	fmt.Fprintf(logFile, "=========================================\n\n")

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get stdin pipe: %v\n", err)
		os.Exit(1)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get stdout pipe: %v\n", err)
		os.Exit(1)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start command: %v\n", err)
		os.Exit(1)
	}

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		fmt.Fprintf(logFile, "[%s] Received signal: %v, propagating to child...\n", timestamp(), sig)
		logFile.Sync()
		if cmd.Process != nil {
			cmd.Process.Signal(sig)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// Proxy stdin: parent -> subprocess, logging each line
	go func() {
		defer wg.Done()
		defer stdinPipe.Close()
		reader := bufio.NewReader(os.Stdin)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					fmt.Fprintf(logFile, "[%s] STDIN ERROR: %v\n", timestamp(), err)
				}
				return
			}
			fmt.Fprintf(logFile, "[%s] --> %s", timestamp(), line)
			logFile.Sync()
			stdinPipe.Write(line)
		}
	}()

	// Proxy stdout: subprocess -> parent, logging each line
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stdoutPipe)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					fmt.Fprintf(logFile, "[%s] STDOUT ERROR: %v\n", timestamp(), err)
				}
				return
			}
			fmt.Fprintf(logFile, "[%s] <-- %s", timestamp(), line)
			logFile.Sync()
			os.Stdout.Write(line)
		}
	}()

	// Wait for child process to exit
	err = cmd.Wait()
	fmt.Fprintf(logFile, "[%s] Child process exited: %v\n", timestamp(), err)
	logFile.Sync()

	// Wait for goroutines to finish
	wg.Wait()

	fmt.Fprintf(logFile, "[%s] === MCP Debug Proxy Shutdown ===\n", timestamp())
}

func timestamp() string {
	return time.Now().Format("15:04:05.000")
}
