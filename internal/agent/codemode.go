package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/gai"
)

// executeTypescriptToolName is the name of the tool exposed to the LLM in code mode
const executeTypescriptToolName = "execute_typescript"

// setupCodeMode initializes the bridge server, generates the TypeScript preamble,
// and returns the execute_typescript tool and its callback.
func setupCodeMode(ctx context.Context, toolsMap map[string]mcp.ToolData) (gai.Tool, gai.ToolCallback, error) {
	// 1. Start Bridge Server
	port, err := startBridgeServer(ctx, toolsMap)
	if err != nil {
		return gai.Tool{}, nil, fmt.Errorf("failed to start bridge server: %w", err)
	}

	// 2. Generate Symbols
	symbols, err := generateSymbols(ctx, toolsMap)
	if err != nil {
		return gai.Tool{}, nil, fmt.Errorf("failed to generate typescript symbols: %w", err)
	}

	// 3. Generate Preamble
	preamble := makePreamble(symbols, port)

	// 4. Generate tool description addendum (types + function signatures)
	var addendum strings.Builder
	
	// Add type definitions
	if typeDefs, ok := symbols[symbolMapTypesKey]; ok {
		addendum.WriteString(typeDefs)
	}
	
	// Add function signatures
	var keys []string
	for k := range symbols {
		if k != symbolMapTypesKey {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	
	for _, name := range keys {
		addendum.WriteString(symbols[name])
		addendum.WriteString("\n")
	}

	// 5. Define execute_typescript tool
	tool := gai.Tool{
		Name: executeTypescriptToolName,
		Description: fmt.Sprintf(`Execute Typescript code in a Deno runtime. 
You have access to a set of pre-defined functions that map to the configured tools. 
Each function returns a Result<T> type.
The code is executed as a standalone script (not a REPL).
Use console.log to print output. 
The final result of the tool call will be the standard output of the script.
If you need to use Node.js APIs, import them with the "node:" prefix (e.g. import path from "node:path";).

The following definitions and functions are available in the global scope:

`+"```typescript"+`
%s
`+"```", addendum.String()),
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"code": {
					Type:        "string",
					Description: "The complete Typescript code to execute.",
				},
			},
			Required: []string{"code"},
		},
	}

	callback := &ExecuteTypescriptCallback{
		Preamble: preamble,
	}

	return tool, callback, nil
}

// ExecuteTypescriptCallback handles the execution of the generated TypeScript code
type ExecuteTypescriptCallback struct {
	Preamble string
}

func (c *ExecuteTypescriptCallback) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	// Parse arguments
	var args struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(parametersJSON, &args); err != nil {
		return gai.Message{
			Role: gai.ToolResult,
			Blocks: []gai.Block{
				{
					ID:           toolCallID,
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str(fmt.Sprintf("Error parsing parameters: %v", err)),
				},
			},
		}, nil
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "cpe-code-*.ts")
	if err != nil {
		return gai.Message{}, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write preamble + code
	if _, err := tmpFile.WriteString(c.Preamble); err != nil {
		return gai.Message{}, fmt.Errorf("failed to write preamble: %w", err)
	}
	if _, err := tmpFile.WriteString("\n// User Code\n"); err != nil {
		return gai.Message{}, fmt.Errorf("failed to write separator: %w", err)
	}
	if _, err := tmpFile.WriteString(args.Code); err != nil {
		return gai.Message{}, fmt.Errorf("failed to write user code: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return gai.Message{}, fmt.Errorf("failed to close temp file: %w", err)
	}

	// Run Deno
	cmd := exec.CommandContext(ctx, "deno", "run", "--allow-all", "--no-prompt", "--check", tmpFile.Name())
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Prepare result
	var content string

	if err != nil {
		// Deno execution failed (compilation error or runtime panic)
		content = stderr.String()
		if content == "" {
			content = fmt.Sprintf("Error running Deno: %v", err)
		}
		// If stdout has partial output, maybe include it? Spec says return stderr as tool result on error.
		// "If Deno exits with non-zero status ... return stderr as the tool result."
		// I'll append stdout if present just in case useful context is there.
		if stdout.String() != "" {
			content += "\n\nSTDOUT:\n" + stdout.String()
		}
		// The tool execution itself didn't fail (the LLM got a result), but the script failed.
		// We return it as ToolResult so LLM can see the error.
		// Note: gai.ToolResult role is correct.
	} else {
		content = stdout.String()
		// Spec says: "The final result of the tool call will be the standard output of the script."
	}

	return gai.Message{
		Role: gai.ToolResult,
		Blocks: []gai.Block{
			{
				ID:           toolCallID,
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str(content),
			},
		},
	}, nil
}

// Bridge Server implementation

func startBridgeServer(ctx context.Context, toolsMap map[string]mcp.ToolData) (int, error) {
	// Create listener on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Decode request
		var req struct {
			ToolName  string          `json:"tool_name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBridgeError(w, fmt.Sprintf("Invalid request: %v", err))
			return
		}

		// Lookup tool
		toolData, ok := toolsMap[req.ToolName]
		if !ok {
			writeBridgeError(w, fmt.Sprintf("Tool not found: %s", req.ToolName))
			return
		}

		// Call tool
		// We use a dummy toolCallID since this is internal
		resultMsg, err := toolData.ToolCallback.Call(r.Context(), req.Arguments, "bridge-call")
		if err != nil {
			writeBridgeError(w, fmt.Sprintf("Tool execution failed: %v", err))
			return
		}

		// Process result
		// We expect a gai.Message with Content blocks
		// We need to extract content and determine if it's an error (gai.Message doesn't strictly have IsError,
		// but typically tool errors are returned as content with maybe some indication?
		// Actually ToolCallback in client.go returns normal text content even for errors usually,
		// unless it returns an actual Go error which we caught above.
		// However, the MCP result might have IsError set.
		// ToolCallback converts MCP result to Blocks. It doesn't preserve IsError explicitly in gai.Message
		// except as content.
		// Wait, `ToolCallback` in `client.go`:
		/*
			result, err := c.ClientSession.CallTool(...)
			if err != nil { ... returns message with error text ... }
			...
			for i, content := range result.Content { ... }
		*/
		// The `IsError` field from `mcp.CallToolResult` is ignored in `ToolCallback`.
		// This is a limitation of the current `ToolCallback` implementation in `client.go` which adapts to `gai`.
		// But here we are bypassing `gai` mostly? No, we are calling `ToolCallback.Call`.

		// Ideally we would access `clientSession` directly, but `ToolData` encapsulates `ToolCallback`.
		// `ToolCallback` returns `gai.Message`.

		// Let's reconstruct the content from `gai.Message`.
		var sb strings.Builder
		for _, block := range resultMsg.Blocks {
			if block.ModalityType == gai.Text {
				sb.WriteString(block.Content.String())
			} else {
				sb.WriteString(fmt.Sprintf("[%s content]", block.ModalityType))
			}
		}
		text := sb.String()

		// Try to parse as JSON
		var contentObj any = text
		var jsonObj any
		if err := json.Unmarshal([]byte(text), &jsonObj); err == nil {
			contentObj = jsonObj
		}

		resp := struct {
			Content any  `json:"content"`
			IsError bool `json:"is_error"`
		}{
			Content: contentObj,
			IsError: false, // We can't easily determine is_error from gai.Message without parsing text?
			// For now assume success if no Go error occurred.
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	server := &http.Server{
		Handler: handler,
	}

	// Run server in goroutine
	go func() {
		// Serve
		go server.Serve(listener)

		// Wait for context cancellation
		<-ctx.Done()
		server.Close()
	}()

	return port, nil
}

func writeBridgeError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"content":  msg,
		"is_error": true,
	})
}

// Preamble Generation

const tsPreambleStatic = `/// <reference types="npm:@types/node" />

// Discriminated union for result handling
type Result<T> =
  | { success: true; value: T }
  | { success: false; error: string };

// Generic structures for the bridge
interface ToolCall<T> {
    tool_name: string;
    arguments: T;
}

interface ToolResult<T> {
    content: string | T;
    is_error: boolean;
}
`

// SymbolMap contains TypeScript type definitions and function signatures.
// The "_types" key contains all type definitions, and each tool name key contains its function signature.
type SymbolMap map[string]string

const symbolMapTypesKey = "_types"

// generateSymbols generates TypeScript type definitions and function signatures for the given tools.
// Returns a SymbolMap with "_types" containing all type definitions and each tool's signature.
func generateSymbols(ctx context.Context, toolsMap map[string]mcp.ToolData) (SymbolMap, error) {
	// Prepare data for the Deno script that generates interfaces
	type ToolInfo struct {
		Name   string `json:"name"`
		Input  any    `json:"input"`
		Output any    `json:"output"`
	}
	var tools []ToolInfo
	// Sort keys for deterministic output
	var keys []string
	for k := range toolsMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		data := toolsMap[name]
		tools = append(tools, ToolInfo{
			Name:   name,
			Input:  stripTitles(data.InputSchema),
			Output: stripTitles(data.OutputSchema),
		})
	}

	jsonData, err := json.Marshal(tools)
	if err != nil {
		return nil, err
	}

	// Write json data to temp file
	tmpJSON, err := os.CreateTemp("", "cpe-tools-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpJSON.Name())
	tmpJSON.Write(jsonData)
	tmpJSON.Close()

	// Write generator script to temp file
	const generatorScript = `
import { compile } from "npm:json-schema-to-typescript";

const text = await Deno.readTextFile(Deno.args[0]);
const data = JSON.parse(text);

for (const tool of data) {
    try {
        // Generate Input Interface
        if (tool.input) {
            // Ensure input is an object type for interface generation
            // json-schema-to-typescript expects a schema
            let schema = tool.input;
            // Simple sanitization if needed
            const ts = await compile(schema, normalizeName(tool.name) + "Input", { bannerComment: "", additionalProperties: false });
            console.log(ts);
        } else {
            console.log("export interface " + normalizeName(tool.name) + "Input {}");
        }

        // Generate Output Interface
        if (tool.output) {
            let schema = tool.output;
            const ts = await compile(schema, normalizeName(tool.name) + "Output", { bannerComment: "", additionalProperties: false });
            console.log(ts);
        } else {
            console.log("export type " + normalizeName(tool.name) + "Output = Record<string, unknown>;");
        }
    } catch (e) {
        console.error("Error generating types for " + tool.name + ": " + e);
        // Fallback to any
        console.log("export type " + normalizeName(tool.name) + "Input = any;");
        console.log("export type " + normalizeName(tool.name) + "Output = any;");
    }
}

function normalizeName(name) {
    // Convert to PascalCase for types
    return name.replace(/(^|_)(\w)/g, (_, sep, char) => char.toUpperCase());
}
`
	tmpScript, err := os.CreateTemp("", "cpe-gen-*.ts")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpScript.Name())
	tmpScript.WriteString(generatorScript)
	tmpScript.Close()

	// Run Deno to generate interfaces
	cmd := exec.CommandContext(ctx, "deno", "run", "--allow-read", "--allow-env", "--allow-net", "--allow-sys", tmpScript.Name(), tmpJSON.Name())
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("failed to generate types: %v, stderr: %s", err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("failed to generate types: %w", err)
	}

	typeDefs := string(out)

	// Generate symbols map
	symbols := make(SymbolMap)
	symbols[symbolMapTypesKey] = typeDefs
	
	for _, name := range keys {
		typeName := toPascalCase(name)
		
		funcSig := fmt.Sprintf(`/**
 * Call tool: %s
 */
async function %s(input: %sInput): Promise<Result<%sOutput>>`, name, normalizeIdentifier(name), typeName, typeName)
		
		symbols[name] = funcSig
	}

	return symbols, nil
}

// makePreamble generates the complete TypeScript preamble including static types,
// server configuration, generated types, and function implementations.
func makePreamble(symbols SymbolMap, port int) string {
	var sb strings.Builder
	sb.WriteString(tsPreambleStatic)
	sb.WriteString(fmt.Sprintf("\nconst SERVER_PORT = %d;\n", port))

	// Add type definitions
	if typeDefs, ok := symbols[symbolMapTypesKey]; ok {
		sb.WriteString(typeDefs)
	}

	// Sort tool names for deterministic output
	var keys []string
	for k := range symbols {
		if k != symbolMapTypesKey {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	// Generate function wrappers
	for _, name := range keys {
		typeName := toPascalCase(name)

		wrapper := fmt.Sprintf(`
/**
 * Call tool: %s
 */
async function %s(input: %sInput): Promise<Result<%sOutput>> {
    const reqBody: ToolCall<%sInput> = {
        tool_name: "%s",
        arguments: input
    };
    
    try {
        const response = await fetch("http://127.0.0.1:" + SERVER_PORT + "/", {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(reqBody),
        });

        if (!response.ok) {
             return { success: false, error: "HTTP error " + response.status };
        }

        const result: ToolResult<%sOutput> = await response.json();

        if (result.is_error) {
            return {
                success: false,
                error: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
            };
        }

        return {
            success: true,
            value: result.content as %sOutput,
        };
    } catch (e) {
        return { success: false, error: String(e) };
    }
}
`, name, normalizeIdentifier(name), typeName, typeName, typeName, name, typeName, typeName)
		sb.WriteString(wrapper)
	}

	return sb.String()
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func toPascalCase(s string) string {
	parts := strings.Split(s, "_")
	for i := range parts {
		parts[i] = capitalize(parts[i])
	}
	return strings.Join(parts, "")
}

func normalizeIdentifier(s string) string {
	// Replace invalid chars with _
	// Start with letter or _
	// This is basic.
	return strings.ReplaceAll(s, "-", "_")
}

func stripTitles(schema any) any {
	if schema == nil {
		return nil
	}

	// Handle json.RawMessage by unmarshalling first
	if raw, ok := schema.(json.RawMessage); ok {
		var v any
		if err := json.Unmarshal(raw, &v); err == nil {
			return stripTitles(v)
		}
		return schema
	}

	switch s := schema.(type) {
	case map[string]any:
		newMap := make(map[string]any)
		for k, v := range s {
			if k == "title" {
				continue
			}
			newMap[k] = stripTitles(v)
		}
		return newMap
	case []any:
		newSlice := make([]any, len(s))
		for i, v := range s {
			newSlice[i] = stripTitles(v)
		}
		return newSlice
	default:
		return s
	}
}
