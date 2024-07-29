package cpe

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"cpe/types"
)

func TestGoplsLSPServer(t *testing.T) {
	// Start gopls server
	port := 8080
	cmd := exec.Command(
		"gopls", "serve", "-port", fmt.Sprintf("%d", port),
		"-rpc.trace",
		"-debug", "localhost:8081",
	)

	// Create a pipe to capture stderr
	stderrPipe, pipeErr := cmd.StderrPipe()
	if pipeErr != nil {
		t.Fatalf("Failed to create stderr pipe: %v", pipeErr)
	}

	// Start reading from stderr in a separate goroutine
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			t.Logf("gopls stderr: %s", scanner.Text())
		}
		if scanErr := scanner.Err(); scanErr != nil {
			t.Errorf("Error reading gopls stderr: %v", scanErr)
		}
	}()

	pipeErr = cmd.Start()
	if pipeErr != nil {
		t.Fatalf("Failed to start gopls server: %v", pipeErr)
	}

	t.Cleanup(
		func() {
			// Kill server
			if killErr := cmd.Process.Kill(); killErr != nil {
				t.Errorf("Failed to kill gopls server: %v", killErr)
			}
		},
	)

	// Wait for the server to start
	time.Sleep(2 * time.Second)

	// Connect to the server
	client, pipeErr := NewClient(fmt.Sprintf("localhost:%d", port))
	if pipeErr != nil {
		t.Fatalf("Failed to connect to gopls server: %v", pipeErr)
	}
	t.Cleanup(
		func() {
			if closeErr := client.Close(); closeErr != nil {
				t.Errorf("Failed to close client: %v", closeErr)
			}
		},
	)

	// Register notification handlers
	notificationHandlers := map[string]func(*types.NotificationMessage){
		types.ShowMessage: func(n *types.NotificationMessage) {
			t.Logf("Received showMessage notification: %+v", n)
		},
		types.LogMessage: func(n *types.NotificationMessage) {
			t.Logf("Received logMessage notification: %+v", n)
		},
		types.PublishDiagnostics: func(n *types.NotificationMessage) {
			t.Logf("Received publishDiagnostics notification: %+v", n)
		},
		"$/progress": func(n *types.NotificationMessage) {
			t.Logf("Received progress notification: %+v", n)
		},
	}

	for method, handler := range notificationHandlers {
		client.RegisterNotificationHandler(method, handler)
	}

	// Get the current directory
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	// Resolve the current directory to an absolute path
	absCurrentDir, err := filepath.Abs(currentDir)
	if err != nil {
		t.Fatalf("Failed to resolve absolute path: %v", err)
	}

	// Convert the absolute path to a file URI
	rootURI := fmt.Sprintf("file://%s", filepath.ToSlash(absCurrentDir))

	// Initialize the server
	initParams := types.InitializeParams{
		ProcessID: new(int),
		RootURI:   rootURI,
		Capabilities: types.ClientCapabilities{
			TextDocument: &types.TextDocumentClientCapabilities{},
			Workspace:    &types.WorkspaceClientCapabilities{},
		},
	}

	initResponse, pipeErr := client.SendRequest("initialize", initParams)
	if pipeErr != nil {
		t.Fatalf("Failed to initialize gopls server: %v", pipeErr)
	}

	// Check if initialization was successful
	if initResponse == nil {
		t.Fatalf("Initialization response is nil")
	}

	var initResult types.InitializeResult
	pipeErr = initResponse.GetResult(&initResult)
	if pipeErr != nil {
		t.Fatalf("Failed to parse initialization result: %v", pipeErr)
	}

	// Print server capabilities
	t.Logf("Server capabilities: %+v", initResult.Capabilities)

	// Send initialized notification
	pipeErr = client.SendNotification("initialized", types.InitializedParams{})
	if pipeErr != nil {
		t.Fatalf("Failed to send initialized notification: %v", pipeErr)
	}

	// Wait for the server to process the initialization
	time.Sleep(2 * time.Second)

	searchForMainSymbol(t, client)

	// Shutdown the server
	shutdownResponse, pipeErr := client.SendRequest("shutdown", nil)
	if pipeErr != nil {
		t.Fatalf("Failed to shutdown gopls server: %v", pipeErr)
	}

	t.Logf("Sent shutdown: %v", shutdownResponse)

	// Ensure that the shutdown response is not nil
	if shutdownResponse == nil {
		t.Fatalf("Shutdown response is nil")
	}

	// Send exit notification
	pipeErr = client.SendNotification("exit", nil)
	if pipeErr != nil {
		t.Fatalf("Failed to send exit notification: %v", pipeErr)
	}

	t.Log("Sent exit")
}

func searchForMainSymbol(t *testing.T, client *Client) {
	params := &types.WorkspaceSymbolParams{
		Query: "main",
	}

	response, err := client.SendRequest("workspace/symbol", params)
	if err != nil {
		t.Fatalf("Failed to search for main symbol: %v", err)
	}

	var symbols []types.SymbolInformation
	err = response.GetResult(&symbols)
	if err != nil {
		t.Fatalf("Failed to parse symbol search result: %v", err)
	}

	found := false
	for _, symbol := range symbols {
		if symbol.Name == "main" && symbol.Kind == types.SymbolKindFunction {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("Main symbol not found")
	}

	t.Logf("Main symbol found successfully")
}
