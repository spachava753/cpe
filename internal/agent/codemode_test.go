package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spachava753/cpe/internal/mcp"
)

func TestMakePreamble(t *testing.T) {
	// Check if deno is installed
	if _, err := exec.LookPath("deno"); err != nil {
		t.Skip("deno not installed, skipping test")
	}

	tests := []struct {
		name     string
		tools    map[string]mcp.ToolData
		port     int
		expected string
	}{
		{
			name: "basic tool with input and output",
			tools: map[string]mcp.ToolData{
				"get_weather": {
					InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"location": { "type": "string" }
						},
						"required": ["location"],
						"additionalProperties": false
					}`),
					OutputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"temperature": { "type": "number" }
						},
						"required": ["temperature"],
						"additionalProperties": false
					}`),
				},
			},
			port: 3000,
			expected: `/// <reference types="npm:@types/node" />

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

const SERVER_PORT = 3000;
export interface GetWeatherInput {
  location: string;
}

export interface GetWeatherOutput {
  temperature: number;
}


/**
 * Call tool: get_weather
 */
async function get_weather(input: GetWeatherInput): Promise<Result<GetWeatherOutput>> {
    const reqBody: ToolCall<GetWeatherInput> = {
        tool_name: "get_weather",
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

        const result: ToolResult<GetWeatherOutput> = await response.json();

        if (result.is_error) {
            return {
                success: false,
                error: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
            };
        }

        return {
            success: true,
            value: result.content as GetWeatherOutput,
        };
    } catch (e) {
        return { success: false, error: String(e) };
    }
}
`,
		},
		{
			name: "tool with no output schema",
			tools: map[string]mcp.ToolData{
				"notify": {
					InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"message": { "type": "string" }
						}
					}`),
					OutputSchema: nil,
				},
			},
			port: 8080,
			expected: `/// <reference types="npm:@types/node" />

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

const SERVER_PORT = 8080;
export interface NotifyInput {
  message?: string;
}

export type NotifyOutput = Record<string, unknown>;

/**
 * Call tool: notify
 */
async function notify(input: NotifyInput): Promise<Result<NotifyOutput>> {
    const reqBody: ToolCall<NotifyInput> = {
        tool_name: "notify",
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

        const result: ToolResult<NotifyOutput> = await response.json();

        if (result.is_error) {
            return {
                success: false,
                error: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
            };
        }

        return {
            success: true,
            value: result.content as NotifyOutput,
        };
    } catch (e) {
        return { success: false, error: String(e) };
    }
}
`,
		},
		{
			name: "complex tool name",
			tools: map[string]mcp.ToolData{
				"my_complex_tool_v2": {
					InputSchema: json.RawMessage(`{"type": "object"}`),
				},
			},
			port: 9000,
			expected: `/// <reference types="npm:@types/node" />

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

const SERVER_PORT = 9000;
export interface MyComplexToolV2Input {}

export type MyComplexToolV2Output = Record<string, unknown>;

/**
 * Call tool: my_complex_tool_v2
 */
async function my_complex_tool_v2(input: MyComplexToolV2Input): Promise<Result<MyComplexToolV2Output>> {
    const reqBody: ToolCall<MyComplexToolV2Input> = {
        tool_name: "my_complex_tool_v2",
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

        const result: ToolResult<MyComplexToolV2Output> = await response.json();

        if (result.is_error) {
            return {
                success: false,
                error: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
            };
        }

        return {
            success: true,
            value: result.content as MyComplexToolV2Output,
        };
    } catch (e) {
        return { success: false, error: String(e) };
    }
}
`,
		},
		{
			name: "multiple tools",
			tools: map[string]mcp.ToolData{
				"tool_a": {InputSchema: json.RawMessage(`{"type":"object"}`)},
				"tool_b": {InputSchema: json.RawMessage(`{"type":"object"}`)},
			},
			port: 8080,
			expected: `/// <reference types="npm:@types/node" />

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

const SERVER_PORT = 8080;
export interface ToolAInput {}

export type ToolAOutput = Record<string, unknown>;
export interface ToolBInput {}

export type ToolBOutput = Record<string, unknown>;

/**
 * Call tool: tool_a
 */
async function tool_a(input: ToolAInput): Promise<Result<ToolAOutput>> {
    const reqBody: ToolCall<ToolAInput> = {
        tool_name: "tool_a",
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

        const result: ToolResult<ToolAOutput> = await response.json();

        if (result.is_error) {
            return {
                success: false,
                error: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
            };
        }

        return {
            success: true,
            value: result.content as ToolAOutput,
        };
    } catch (e) {
        return { success: false, error: String(e) };
    }
}

/**
 * Call tool: tool_b
 */
async function tool_b(input: ToolBInput): Promise<Result<ToolBOutput>> {
    const reqBody: ToolCall<ToolBInput> = {
        tool_name: "tool_b",
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

        const result: ToolResult<ToolBOutput> = await response.json();

        if (result.is_error) {
            return {
                success: false,
                error: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
            };
        }

        return {
            success: true,
            value: result.content as ToolBOutput,
        };
    } catch (e) {
        return { success: false, error: String(e) };
    }
}
`,
		},
		{
			name: "tool with invalid schema (fallback)",
			tools: map[string]mcp.ToolData{
				"broken_tool": {
					InputSchema: json.RawMessage(`{"type": "invalid-type"}`),
				},
			},
			port: 8080,
			expected: `/// <reference types="npm:@types/node" />

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

const SERVER_PORT = 8080;
export interface BrokenToolInput {
  [k: string]: unknown;
}

export type BrokenToolOutput = Record<string, unknown>;

/**
 * Call tool: broken_tool
 */
async function broken_tool(input: BrokenToolInput): Promise<Result<BrokenToolOutput>> {
    const reqBody: ToolCall<BrokenToolInput> = {
        tool_name: "broken_tool",
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

        const result: ToolResult<BrokenToolOutput> = await response.json();

        if (result.is_error) {
            return {
                success: false,
                error: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
            };
        }

        return {
            success: true,
            value: result.content as BrokenToolOutput,
        };
    } catch (e) {
        return { success: false, error: String(e) };
    }
}
`,
		},
		{
			name: "tools with colliding titles in schema",
			tools: map[string]mcp.ToolData{
				"web_search_preview": {
					InputSchema: json.RawMessage(`{
					  "properties": {
						"objective": {
						  "description": "...",
						  "title": "Objective",
						  "type": "string"
						},
						"search_queries": {
						  "description": "...",
						  "items": { "type": "string" },
						  "title": "Search Queries",
						  "type": "array"
						}
					  },
					  "required": ["objective", "search_queries"],
					  "title": "web_search_previewArguments",
					  "type": "object"
					}`),
				},
				"web_fetch": {
					InputSchema: json.RawMessage(`{
					  "properties": {
						"objective": {
						  "anyOf": [
							{ "type": "string" },
							{ "type": "null" }
						  ],
						  "default": null,
						  "description": "...",
						  "title": "Objective"
						},
						"urls": {
						  "description": "...",
						  "items": { "type": "string" },
						  "title": "Urls",
						  "type": "array"
						}
					  },
					  "required": ["urls"],
					  "title": "extract_toolArguments",
					  "type": "object"
					}`),
				},
			},
			port: 3000,
			expected: `/// <reference types="npm:@types/node" />

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

const SERVER_PORT = 3000;
export interface WebFetchInput {
  /**
   * ...
   */
  objective?: string | null;
  /**
   * ...
   */
  urls: string[];
}

export type WebFetchOutput = Record<string, unknown>;
export interface WebSearchPreviewInput {
  /**
   * ...
   */
  objective: string;
  /**
   * ...
   */
  search_queries: string[];
}

export type WebSearchPreviewOutput = Record<string, unknown>;

/**
 * Call tool: web_fetch
 */
async function web_fetch(input: WebFetchInput): Promise<Result<WebFetchOutput>> {
    const reqBody: ToolCall<WebFetchInput> = {
        tool_name: "web_fetch",
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

        const result: ToolResult<WebFetchOutput> = await response.json();

        if (result.is_error) {
            return {
                success: false,
                error: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
            };
        }

        return {
            success: true,
            value: result.content as WebFetchOutput,
        };
    } catch (e) {
        return { success: false, error: String(e) };
    }
}

/**
 * Call tool: web_search_preview
 */
async function web_search_preview(input: WebSearchPreviewInput): Promise<Result<WebSearchPreviewOutput>> {
    const reqBody: ToolCall<WebSearchPreviewInput> = {
        tool_name: "web_search_preview",
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

        const result: ToolResult<WebSearchPreviewOutput> = await response.json();

        if (result.is_error) {
            return {
                success: false,
                error: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
            };
        }

        return {
            success: true,
            value: result.content as WebSearchPreviewOutput,
        };
    } catch (e) {
        return { success: false, error: String(e) };
    }
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			symbols, err := generateSymbols(ctx, tt.tools)
			if err != nil {
				t.Fatalf("generateSymbols() error = %v", err)
			}

			got := makePreamble(symbols, tt.port)

			if diff := cmp.Diff(tt.expected, got); diff != "" {
				t.Errorf("makePreamble() mismatch (-want +got):\n%s", diff)
			}

			// Verify that the generated code compiles
			tmpFile, err := os.CreateTemp("", "preamble-check-*.ts")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(got); err != nil {
				t.Fatalf("failed to write generated code to temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("failed to close temp file: %v", err)
			}

			cmd := exec.Command("deno", "check", tmpFile.Name())
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("generated preamble failed to compile: %v\noutput: %s", err, out)
			}
		})
	}
}