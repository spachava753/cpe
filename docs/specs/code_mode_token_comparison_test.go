package specs

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/go-faker/faker/v4"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
)

// TestCodeModeTokenComparison compares token usage between code mode and normal tool calling
// using the examples from docs/specs/code_mode.md.
//
// This test requires ANTHROPIC_API_KEY environment variable to be set.
//
//go:embed testdata/virtual_tool_sample_code.txt
var virtualToolSampleCode string

//go:embed testdata/virtual_tool_call_example.txt
var virtualToolCallExample string

func TestCodeModeTokenComparison(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping token comparison test")
	}

	ctx := context.Background()

	// Define the tools from the spec example
	getWeatherTool := &mcp.Tool{
		Name:        "get_weather",
		Description: "Get current weather data for a location",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{
					"type":        "string",
					"description": "The name of the city to get weather for",
				},
				"unit": map[string]any{
					"type":        "string",
					"enum":        []any{"celsius", "fahrenheit"},
					"description": "Temperature unit for the weather response",
				},
			},
			"required": []any{"city", "unit"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"temperature": map[string]any{
					"type":        "number",
					"description": "Temperature in celsius",
				},
			},
			"required": []any{"temperature"},
		},
	}

	getCityTool := &mcp.Tool{
		Name:        "get_city",
		Description: "Get current city location",
		InputSchema: map[string]any{},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{
					"type":        "string",
					"description": "Current city location",
				},
			},
			"required": []any{"city"},
		},
	}

	t.Run("simple_two_tool_composition", func(t *testing.T) {
		client := anthropic.NewClient()
		svc := gai.NewAnthropicServiceWrapper(&client.Messages)
		gen := gai.NewAnthropicGenerator(svc, "claude-sonnet-4-20250514", "")

		if err := gen.Register(convertMcpToGaiTool(getWeatherTool)); err != nil {
			t.Fatalf("failed to register get_weather tool: %v", err)
		}
		if err := gen.Register(convertMcpToGaiTool(getCityTool)); err != nil {
			t.Fatalf("failed to register get_city tool: %v", err)
		}

		normalModeDialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("What is the current temperature?")}},
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Let me get the current location and then the temperature"),
				mustToolCallBlock(t, "tool_1", "get_city", map[string]any{}),
			}},
			gai.ToolResultMessage("tool_1", gai.TextBlock(`{"city": "New York"}`)),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("I have the city, now I will get the temperature"),
				mustToolCallBlock(t, "tool_2", "get_weather", map[string]any{
					"city": "New York",
					"unit": "fahrenheit",
				}),
			}},
			gai.ToolResultMessage("tool_2", gai.TextBlock(`{"temperature": 86}`)),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("It is 86 degree fahrenheit in New York City."),
			}},
		}

		normalTokens, err := gen.Count(ctx, normalModeDialog)
		if err != nil {
			t.Fatalf("failed to count normal mode tokens: %v", err)
		}

		codeModeClient := anthropic.NewClient()
		codeModeSvc := gai.NewAnthropicServiceWrapper(&codeModeClient.Messages)
		codeModeGen := gai.NewAnthropicGenerator(codeModeSvc, "claude-sonnet-4-20250514", "")

		executeGoCodeTool, err := codemode.GenerateExecuteGoCodeTool([]*mcp.Tool{getCityTool, getWeatherTool}, 300)
		if err != nil {
			t.Fatalf("failed to generate execute_go_code tool: %v", err)
		}
		if err := codeModeGen.Register(executeGoCodeTool); err != nil {
			t.Fatalf("failed to register execute_go_code tool: %v", err)
		}

		generatedCode := `package main

import (
	"context"
	"fmt"
)

func Run(ctx context.Context) error {
	city, err := GetCity(ctx)
	if err != nil {
		return err
	}

	temp, err := GetWeather(ctx, GetWeatherInput{
		City: city.City,
		Unit: "fahrenheit",
	})
	if err != nil {
		return err
	}

	fmt.Printf("Temperature: %f, City: %s\n", temp.Temperature, city.City)
	return nil
}
`

		codeModeDialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("What is the current temperature?")}},
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Let me get the current location and then the temperature"),
				mustToolCallBlock(t, "tool_1", "execute_go_code", map[string]any{
					"code":             generatedCode,
					"executionTimeout": 30,
				}),
			}},
			gai.ToolResultMessage("tool_1", gai.TextBlock("Temperature: 86.000000, City: New York\n")),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("It is 86 degree fahrenheit in New York City."),
			}},
		}

		codeModeTokens, err := codeModeGen.Count(ctx, codeModeDialog)
		if err != nil {
			t.Fatalf("failed to count code mode tokens: %v", err)
		}

		logComparison(t, "Simple Two-Tool Composition", normalTokens, codeModeTokens)
	})

	t.Run("file_io_with_loops_5_cities", func(t *testing.T) {
		readFileTool := &mcp.Tool{
			Name:        "read_file",
			Description: "Read the contents of a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Path to the file to read",
					},
				},
				"required": []any{"path"},
			},
		}

		client := anthropic.NewClient()
		svc := gai.NewAnthropicServiceWrapper(&client.Messages)
		gen := gai.NewAnthropicGenerator(svc, "claude-sonnet-4-20250514", "")

		if err := gen.Register(convertMcpToGaiTool(getWeatherTool)); err != nil {
			t.Fatalf("failed to register get_weather tool: %v", err)
		}
		if err := gen.Register(convertMcpToGaiTool(readFileTool)); err != nil {
			t.Fatalf("failed to register read_file tool: %v", err)
		}

		normalModeDialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("Get the weather for each city in cities.txt")}},
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Let me read the file first"),
				mustToolCallBlock(t, "tool_1", "read_file", map[string]any{"path": "cities.txt"}),
			}},
			gai.ToolResultMessage("tool_1", gai.TextBlock("New York\nLos Angeles\nChicago\nMiami\nSeattle")),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("I'll get the weather for New York first"),
				mustToolCallBlock(t, "tool_2", "get_weather", map[string]any{"city": "New York", "unit": "fahrenheit"}),
			}},
			gai.ToolResultMessage("tool_2", gai.TextBlock(`{"temperature": 72}`)),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Now Los Angeles"),
				mustToolCallBlock(t, "tool_3", "get_weather", map[string]any{"city": "Los Angeles", "unit": "fahrenheit"}),
			}},
			gai.ToolResultMessage("tool_3", gai.TextBlock(`{"temperature": 85}`)),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Now Chicago"),
				mustToolCallBlock(t, "tool_4", "get_weather", map[string]any{"city": "Chicago", "unit": "fahrenheit"}),
			}},
			gai.ToolResultMessage("tool_4", gai.TextBlock(`{"temperature": 68}`)),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Now Miami"),
				mustToolCallBlock(t, "tool_5", "get_weather", map[string]any{"city": "Miami", "unit": "fahrenheit"}),
			}},
			gai.ToolResultMessage("tool_5", gai.TextBlock(`{"temperature": 88}`)),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Finally Seattle"),
				mustToolCallBlock(t, "tool_6", "get_weather", map[string]any{"city": "Seattle", "unit": "fahrenheit"}),
			}},
			gai.ToolResultMessage("tool_6", gai.TextBlock(`{"temperature": 62}`)),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Here's the weather for each city:\n- New York: 72°F\n- Los Angeles: 85°F\n- Chicago: 68°F\n- Miami: 88°F\n- Seattle: 62°F"),
			}},
		}

		normalTokens, err := gen.Count(ctx, normalModeDialog)
		if err != nil {
			t.Fatalf("failed to count normal mode tokens: %v", err)
		}

		codeModeClient := anthropic.NewClient()
		codeModeSvc := gai.NewAnthropicServiceWrapper(&codeModeClient.Messages)
		codeModeGen := gai.NewAnthropicGenerator(codeModeSvc, "claude-sonnet-4-20250514", "")

		executeGoCodeTool, err := codemode.GenerateExecuteGoCodeTool([]*mcp.Tool{getWeatherTool}, 300)
		if err != nil {
			t.Fatalf("failed to generate execute_go_code tool: %v", err)
		}
		if err := codeModeGen.Register(executeGoCodeTool); err != nil {
			t.Fatalf("failed to register execute_go_code tool: %v", err)
		}

		generatedCode := `package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

func Run(ctx context.Context) error {
	file, err := os.Open("cities.txt")
	if err != nil {
		return err
	}
	defer file.Close()

	var results []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		city := strings.TrimSpace(scanner.Text())
		if city == "" {
			continue
		}

		weather, err := GetWeather(ctx, GetWeatherInput{
			City: city,
			Unit: "fahrenheit",
		})
		if err != nil {
			return fmt.Errorf("failed to get weather for %s: %w", city, err)
		}

		results = append(results, fmt.Sprintf("%s: %.0f°F", city, *weather.Temperature))
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	fmt.Println(strings.Join(results, "\n"))
	return nil
}
`

		codeModeDialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("Get the weather for each city in cities.txt")}},
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("I'll read the file and get weather for all cities in one go"),
				mustToolCallBlock(t, "tool_1", "execute_go_code", map[string]any{
					"code":             generatedCode,
					"executionTimeout": 60,
				}),
			}},
			gai.ToolResultMessage("tool_1", gai.TextBlock("New York: 72°F\nLos Angeles: 85°F\nChicago: 68°F\nMiami: 88°F\nSeattle: 62°F\n")),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Here's the weather for each city:\n- New York: 72°F\n- Los Angeles: 85°F\n- Chicago: 68°F\n- Miami: 88°F\n- Seattle: 62°F"),
			}},
		}

		codeModeTokens, err := codeModeGen.Count(ctx, codeModeDialog)
		if err != nil {
			t.Fatalf("failed to count code mode tokens: %v", err)
		}

		logComparison(t, "File I/O with Loops (5 Cities)", normalTokens, codeModeTokens)
	})

	t.Run("tool_definition_overhead_only", func(t *testing.T) {
		readFileTool := &mcp.Tool{
			Name:        "read_file",
			Description: "Read the contents of a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Path to the file to read",
					},
				},
				"required": []any{"path"},
			},
		}

		client := anthropic.NewClient()
		svc := gai.NewAnthropicServiceWrapper(&client.Messages)
		gen := gai.NewAnthropicGenerator(svc, "claude-sonnet-4-20250514", "")

		if err := gen.Register(convertMcpToGaiTool(getWeatherTool)); err != nil {
			t.Fatalf("failed to register get_weather tool: %v", err)
		}
		if err := gen.Register(convertMcpToGaiTool(getCityTool)); err != nil {
			t.Fatalf("failed to register get_city tool: %v", err)
		}
		if err := gen.Register(convertMcpToGaiTool(readFileTool)); err != nil {
			t.Fatalf("failed to register read_file tool: %v", err)
		}

		normalModeDialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hi")}},
		}

		normalTokens, err := gen.Count(ctx, normalModeDialog)
		if err != nil {
			t.Fatalf("failed to count normal mode tokens: %v", err)
		}

		codeModeClient := anthropic.NewClient()
		codeModeSvc := gai.NewAnthropicServiceWrapper(&codeModeClient.Messages)
		codeModeGen := gai.NewAnthropicGenerator(codeModeSvc, "claude-sonnet-4-20250514", "")

		executeGoCodeTool, err := codemode.GenerateExecuteGoCodeTool([]*mcp.Tool{getCityTool, getWeatherTool}, 300)
		if err != nil {
			t.Fatalf("failed to generate execute_go_code tool: %v", err)
		}
		if err := codeModeGen.Register(executeGoCodeTool); err != nil {
			t.Fatalf("failed to register execute_go_code tool: %v", err)
		}

		codeModeDialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hi")}},
		}

		codeModeTokens, err := codeModeGen.Count(ctx, codeModeDialog)
		if err != nil {
			t.Fatalf("failed to count code mode tokens: %v", err)
		}

		t.Logf("")
		t.Logf("=== Tool Definition Overhead (3 tools) ===")
		t.Logf("  Normal mode tool definitions: %d tokens", normalTokens)
		t.Logf("  Code mode tool description:   %d tokens", codeModeTokens)
		t.Logf("  Overhead:                     %d tokens", int(codeModeTokens)-int(normalTokens))
		t.Logf("")
		t.Logf("Note: Code mode has higher base overhead from execute_go_code description,")
		t.Logf("but saves tokens by not including intermediate tool results in context.")
	})

	t.Run("massive_file_avoids_context_bloat", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "names-*.txt")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		targetName := "Alice Johnson"
		for i := 0; i < 1200; i++ {
			var name string
			if i == 600 {
				name = targetName
			} else {
				name = fmt.Sprintf("%s %s", faker.FirstName(), faker.LastName())
			}
			if _, err := fmt.Fprintln(tmpFile, name); err != nil {
				t.Fatalf("failed to write to temp file: %v", err)
			}
		}
		tmpFile.Close()

		fileContent, err := os.ReadFile(tmpFile.Name())
		if err != nil {
			t.Fatalf("failed to read temp file: %v", err)
		}
		fileContentStr := string(fileContent)

		readFileTool := &mcp.Tool{
			Name:        "read_file",
			Description: "Read the contents of a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Path to the file to read",
					},
				},
				"required": []any{"path"},
			},
		}

		client := anthropic.NewClient()
		svc := gai.NewAnthropicServiceWrapper(&client.Messages)
		gen := gai.NewAnthropicGenerator(svc, "claude-sonnet-4-20250514", "")

		if err := gen.Register(convertMcpToGaiTool(readFileTool)); err != nil {
			t.Fatalf("failed to register read_file tool: %v", err)
		}

		normalModeDialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock(fmt.Sprintf("Does the name '%s' exist in %s?", targetName, tmpFile.Name()))}},
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Let me read the file to check"),
				mustToolCallBlock(t, "tool_1", "read_file", map[string]any{"path": tmpFile.Name()}),
			}},
			gai.ToolResultMessage("tool_1", gai.TextBlock(fileContentStr)),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock(fmt.Sprintf("Yes, '%s' exists in the file.", targetName)),
			}},
		}

		normalTokens, err := gen.Count(ctx, normalModeDialog)
		if err != nil {
			t.Fatalf("failed to count normal mode tokens: %v", err)
		}

		codeModeClient := anthropic.NewClient()
		codeModeSvc := gai.NewAnthropicServiceWrapper(&codeModeClient.Messages)
		codeModeGen := gai.NewAnthropicGenerator(codeModeSvc, "claude-sonnet-4-20250514", "")

		executeGoCodeTool, err := codemode.GenerateExecuteGoCodeTool([]*mcp.Tool{}, 300)
		if err != nil {
			t.Fatalf("failed to generate execute_go_code tool: %v", err)
		}
		if err := codeModeGen.Register(executeGoCodeTool); err != nil {
			t.Fatalf("failed to register execute_go_code tool: %v", err)
		}

		generatedCode := fmt.Sprintf(`package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

func Run(ctx context.Context) error {
	file, err := os.Open("%s")
	if err != nil {
		return err
	}
	defer file.Close()

	targetName := "%s"
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == targetName {
			fmt.Println("Yes")
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	fmt.Println("No")
	return nil
}
`, tmpFile.Name(), targetName)

		codeModeDialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock(fmt.Sprintf("Does the name '%s' exist in %s?", targetName, tmpFile.Name()))}},
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("I'll write code to search the file"),
				mustToolCallBlock(t, "tool_1", "execute_go_code", map[string]any{
					"code":             generatedCode,
					"executionTimeout": 30,
				}),
			}},
			gai.ToolResultMessage("tool_1", gai.TextBlock("Yes\n")),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock(fmt.Sprintf("Yes, '%s' exists in the file.", targetName)),
			}},
		}

		codeModeTokens, err := codeModeGen.Count(ctx, codeModeDialog)
		if err != nil {
			t.Fatalf("failed to count code mode tokens: %v", err)
		}

		approxFileTokens := len(fileContentStr) / 4

		t.Logf("")
		t.Logf("=== Massive File Search (~%d token file) ===", approxFileTokens)
		t.Logf("  Normal tool calling: %d tokens", normalTokens)
		t.Logf("  Code mode:           %d tokens", codeModeTokens)
		t.Logf("  Difference:          %d tokens", int(normalTokens)-int(codeModeTokens))

		if codeModeTokens < normalTokens {
			savings := float64(normalTokens-codeModeTokens) / float64(normalTokens) * 100
			t.Logf("  Savings:             %.1f%%", savings)
			t.Logf("")
			t.Logf("SUCCESS! Code mode saves tokens by processing the file without")
			t.Logf("sending its contents through the LLM context.")
		} else {
			t.Errorf("Expected code mode to save tokens with massive file, but it used %d more", codeModeTokens-normalTokens)
		}
	})

	t.Run("traditional_vs_virtual_tool_calling", func(t *testing.T) {
		// This test compares token efficiency between:
		// 1. Traditional tool calling: code is passed as a JSON string parameter
		// 2. Virtual tool calling: code is wrapped in XML tags as plain text
		//
		// Both use the same tool description for fair comparison.

		// Generate the tool description (same for both modes)
		executeGoCodeTool, err := codemode.GenerateExecuteGoCodeTool([]*mcp.Tool{getWeatherTool, getCityTool}, 300)
		if err != nil {
			t.Fatalf("failed to generate execute_go_code tool: %v", err)
		}

		// === Traditional tool calling ===
		client := anthropic.NewClient()
		svc := gai.NewAnthropicServiceWrapper(&client.Messages)
		gen := gai.NewAnthropicGenerator(svc, "claude-sonnet-4-20250514", "")

		if err := gen.Register(executeGoCodeTool); err != nil {
			t.Fatalf("failed to register execute_go_code tool: %v", err)
		}

		traditionalDialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("Get the weather for each city in cities.txt")}},
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("I'll write code to read the file and get weather for all cities."),
				mustToolCallBlock(t, "tool_1", "execute_go_code", map[string]any{
					"code":             virtualToolSampleCode,
					"executionTimeout": 60,
				}),
			}},
			gai.ToolResultMessage("tool_1", gai.TextBlock("Weather Report\n==============\nNew York: 72°F\nLos Angeles: 85°F\nChicago: 68°F\n")),
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Here's the weather report for all cities."),
			}},
		}

		traditionalTokens, err := gen.Count(ctx, traditionalDialog)
		if err != nil {
			t.Fatalf("failed to count traditional mode tokens: %v", err)
		}

		// === Virtual tool calling ===
		// Uses the same tool description but in the system prompt instead of as a registered tool
		virtualClient := anthropic.NewClient()
		virtualSvc := gai.NewAnthropicServiceWrapper(&virtualClient.Messages)
		virtualGen := gai.NewAnthropicGenerator(virtualSvc, "claude-sonnet-4-20250514", executeGoCodeTool.Description)

		virtualDialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("Get the weather for each city in cities.txt")}},
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock(virtualToolCallExample),
			}},
			{Role: gai.User, Blocks: []gai.Block{
				gai.TextBlock("<tool_result>\nWeather Report\n==============\nNew York: 72°F\nLos Angeles: 85°F\nChicago: 68°F\n</tool_result>"),
			}},
			{Role: gai.Assistant, Blocks: []gai.Block{
				gai.TextBlock("Here's the weather report for all cities."),
			}},
		}

		virtualTokens, err := virtualGen.Count(ctx, virtualDialog)
		if err != nil {
			t.Fatalf("failed to count virtual mode tokens: %v", err)
		}

		t.Logf("")
		t.Logf("=== Traditional vs Virtual Tool Calling ===")
		t.Logf("  Traditional (JSON tool call): %d tokens", traditionalTokens)
		t.Logf("  Virtual (XML-delimited text): %d tokens", virtualTokens)
		t.Logf("  Difference:                   %d tokens", int(traditionalTokens)-int(virtualTokens))

		if virtualTokens < traditionalTokens {
			savings := float64(traditionalTokens-virtualTokens) / float64(traditionalTokens) * 100
			t.Logf("  Savings:                      %.1f%% (virtual is more efficient)", savings)
			t.Logf("")
			t.Logf("Virtual tool calling saves tokens by avoiding JSON escaping overhead")
			t.Logf("(newlines, quotes, special characters don't need escaping in XML/text).")
		} else if virtualTokens > traditionalTokens {
			overhead := float64(virtualTokens-traditionalTokens) / float64(traditionalTokens) * 100
			t.Logf("  Overhead:                     %.1f%% (traditional is more efficient)", overhead)
			t.Logf("")
			t.Logf("Traditional tool calling is more efficient in this case.")
		} else {
			t.Logf("")
			t.Logf("Both approaches use the same number of tokens.")
		}
	})
}

func logComparison(t *testing.T, scenario string, normalTokens, codeModeTokens uint) {
	t.Helper()
	t.Logf("")
	t.Logf("=== %s ===", scenario)
	t.Logf("  Normal tool calling: %d tokens", normalTokens)
	t.Logf("  Code mode:           %d tokens", codeModeTokens)
	t.Logf("  Difference:          %d tokens", int(normalTokens)-int(codeModeTokens))

	if codeModeTokens < normalTokens {
		savings := float64(normalTokens-codeModeTokens) / float64(normalTokens) * 100
		t.Logf("  Savings:             %.1f%%", savings)
	} else {
		overhead := float64(codeModeTokens-normalTokens) / float64(normalTokens) * 100
		t.Logf("  Overhead:            %.1f%% (code mode uses more in this scenario)", overhead)
	}
}

func convertMcpToGaiTool(mcpTool *mcp.Tool) gai.Tool {
	var inputSchema *jsonschema.Schema
	if mcpTool.InputSchema != nil {
		inputSchema = convertToJsonSchema(mcpTool.InputSchema)
	}

	return gai.Tool{
		Name:        mcpTool.Name,
		Description: mcpTool.Description,
		InputSchema: inputSchema,
	}
}

func convertToJsonSchema(schema any) *jsonschema.Schema {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return nil
	}

	result := &jsonschema.Schema{}

	if t, ok := schemaMap["type"].(string); ok {
		result.Type = t
	}

	if desc, ok := schemaMap["description"].(string); ok {
		result.Description = desc
	}

	if props, ok := schemaMap["properties"].(map[string]any); ok {
		result.Properties = make(map[string]*jsonschema.Schema)
		for name, propVal := range props {
			if propMap, ok := propVal.(map[string]any); ok {
				result.Properties[name] = convertToJsonSchema(propMap)
			}
		}
	}

	if required, ok := schemaMap["required"].([]any); ok {
		for _, r := range required {
			if s, ok := r.(string); ok {
				result.Required = append(result.Required, s)
			}
		}
	}

	if enum, ok := schemaMap["enum"].([]any); ok {
		result.Enum = enum
	}

	return result
}

func mustToolCallBlock(t *testing.T, id, name string, params map[string]any) gai.Block {
	t.Helper()
	block, err := gai.ToolCallBlock(id, name, params)
	if err != nil {
		t.Fatalf("failed to create tool call block: %v", err)
	}
	return block
}

