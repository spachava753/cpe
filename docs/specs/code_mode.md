# CPE Code Mode Specification

This document outlines the design for "Code Mode" in CPE, a feature that allows LLMs to execute Golang code to interact with MCP tools and the user's system in a composable and type-safe manner.

## Overview

Currently, LLMs interact with tools via discrete tool calls. This has limitations:
1.  **Context Usage**: Tool schemas and descriptions consume significant context.
2.  **Composability**: Chaining tools requires multiple round-trips (LLM -> Tool -> LLM -> Tool), increasing latency and cost.
3.  **Expressiveness**: LLMs are restricted to the tool's specific API without logic or control flow.

"Code Mode" solves this by exposing MCP tools and APIs as strongly-typed Golang functions. The LLM generates Golang code that calls these functions, allowing it to perform complex logic, data processing, and composed tool executions in a single turn.

## Example

Given a simple AI agent configuration of two MCP tools that can compose together, `get_weather`, and `get_city`, here is an example that showcases the difference between code mode and normal tool calling.

### `get_weather` tool

```json
{
  "name": "get_weather",
  "title": "Weather Data Retriever",
  "description": "Get current weather data for a location",
  "inputSchema": {
    "type": "object",
    "properties": {
      "city": {
        "type": "string",
        "description": "The name of the city to get weather for"
      },
      "unit": {
        "type": "string",
        "enum": ["celsius", "fahrenheit"],
        "description": "Temperature unit for the weather response"
      }
    },
    "required": ["location", "unit"]
  },
  "outputSchema": {
    "type": "object",
    "properties": {
      "temperature": {
        "type": "number",
        "description": "Temperature in celsius"
      }
    },
    "required": ["temperature"]
  }
}
```

### `get_city` tool

```json
{
  "name": "get_city",
  "title": "City location retrieval",
  "description": "Get current city location",
  "inputSchema": {},
  "outputSchema": {
    "type": "object",
    "properties": {
      "city": {
        "type": "string",
        "description": "Current city location"
      }
    },
    "required": ["city"]
  }
}
```

### Normal tool call

#### User

What is the current temperature?

#### Assistant

Thinking: Let me get the current location and then the temperature
Calling Tool:
```json
{
  "name": "get_city",
  "parameters": {}
}
```

#### Tool Result

```json
{
  "city": "New York"
}
```

#### Assistant

Thinking: I have the city, now I will get the temperature
Calling Tool:
```json
{
  "name": "get_weather",
  "parameters": {
    "city": "New York",
    "unit": "fahrenheit"
  }
}
```

#### Tool Result

```json
{
  "temperature": 86
}
```

#### Assistant

It is 86 degree fahrenheit in New York City.

### Code Mode

#### User

What is the current temperature?

#### Assistant

Thinking: Let me get the current location and then the temperature
Calling Tool:
```json
{
  "name": "execute_go_code",
  "parameters": {
    "code": "city, err := getCity(ctx)\nif err != nil {\n\t\treturn err\n}\n\ntemp, err := getWeather(ctx, GetWeatherInput{\n\t\tCity: city,\n\t\tUnit: GetWeatherInputUnitFahrenheit\n})\nif err != nil {\n\t\treturn err\n}\n\nfmt.Printf(\"Temperature: %f, City: %s\\n\", temp, city)"
  }
}
```

#### Tool Result

```json
{
  "output": "Temperature: 87, City: New York\n"
}
```

#### Assistant

It is 86 degree fahrenheit in New York City.

### Comparison

Notice how "code mode" resulted in less overall turns in the conversation, reducing latency to end result. It also is more token efficient, as there isn't much tool call arguments and tool result JSON that would feed into the LLM. Over time, in a long conversation, especially with many tools, this benefit compounds.

## Architecture

### 1. The "Execute Go Code" Tool

When Code Mode is enabled, CPE hides the individual MCP tools from the LLM. Instead, it exposes a single tool:

```json
{
  "name": "execute_go_code",
  "title": "Execute Golang Code",
  "description": "See below section",
  "inputSchema": {
    "type": "object",
    "properties": {
      "code": {
        "type": "string",
        "description": "The code the execute"
      },
    },
    "required": ["code"]
  },
  "outputSchema": {
    "type": "object",
    "properties": {
      "output": {
        "type": "string",
        "description": "The program output"
      }
    },
    "required": ["output"]
  }
}
```

The description of the tool will be:
````markdown
Execute generated Golang code. The generated code will be inlined into a function with signature of `Run() err`. As such, you cannot define functions normally, but must assign them to a variables like so:

```go
add := func(a, b int) int {
	return a + b
}
fmt.Println(add(1, 2))
```

In addition to the standard library, you have access to the following functions:
```go
type GetWeatherInputUnit string
const GetWeatherInputUnitFahrenheit GetWeatherInputUnit = "fahrenheit"
const GetWeatherInputUnitCelsius GetWeatherInputUnit = "celsius"

type GetWeatherInput struct {
	// City The name of the city to get weather for
	City string `json:"city"`
	// Unit Temperature unit for the weather response
	Unit GetWeatherInputUnit `json:"unit"`
}

type GetWeatherOutput struct {
	// Temperature Temperature in celsius
	Temperature float64 `json:"temperature"`
}

// getWeather Get current weather data for a location
var getWeather func(ctx context.Context, input GetWeatherInput) (GetWeatherOutput, error)
```
````

### 2. Function Generation & Type Mapping

At runtime (during `CreateToolCapableGenerator` initialization), CPE introspects the configured MCP tools to generate Golang structs, function definitions, which are added to the tools description. In addition, the types and full function code are prepended to the generated code that the LLM generates, as a "preamble".

**Code Preamble:**
```go
type GetWeatherInputUnit string
const GetWeatherInputUnitFahrenheit GetWeatherInputUnit = "fahrenheit"
const GetWeatherInputUnitCelsius GetWeatherInputUnit = "celsius"

type GetWeatherInput struct {
	// City The name of the city to get weather for
	City string `json:"city"`
	// Unit Temperature unit for the weather response
	Unit GetWeatherInputUnit `json:"unit"`
}

type GetWeatherOutput struct {
	// Temperature Temperature in celsius
	Temperature float64 `json:"temperature"`
}

// getWeather Get current weather data for a location
var getWeather func(ctx context.Context, input GetWeatherInput) (GetWeatherOutput, error)

// callMcpTool is a reusable utility function for calling a mcp tool
func callMcpTool[I any, O any](ctx context.Context, clientSession *mcp.ClientSession, toolName string, input T) (O, error) {
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: input})
	if err != nil {
		panic(fmt.Sprintf("error calling tool %s: %s", toolName, err))
	}
	
	if len(res.Content) != 1 {
		panic(fmt.Sprintf("expected number of content parts from tool %s: %d", toolName, len(res.Content)))
	}
	
	textContent := res.Content[0].(*mcp.TextContent).Text
	var output O
	
	if result.IsError {
		return output, errors.New(textContent)
	}
	
	outputJson := []byte(textContent)
	
	if result.StructuredContent != nil {
		structuredContent, err := json.Marshal(result.StructuredContent)
		if err != nil {
			panic(fmt.Sprintf("could not marshal structured content: %s", err))
		}
		outputJson = structuredContent
	}
	
	if err := json.Unmarshal(outputJson, &output); err != nil {
		panic(fmt.Sprintf("could not unmarshal structured content json into output for tool get_weather: %s", err))
	}
	return output, nil
}

func Run() err {
	// init mcp client
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil)
	// create transports and connect to mcp servers
	
	// example stdio server
	exampleStdioServerCmd := exec.Command("example-stdio-server", "arg1", "arg2")
	exampleStdioServerTransport = &mcp.CommandTransport{Command: exampleStdioServerCmd}
	exampleStdioServerSession, err := mcpClient.Connect(ctx, exampleStdioServerTransport, nil)
	if err != nil {
		panic(fmt.Sprintf("could not connect to example-stdio-server server: %s", err))
	}
	defer exampleStdioServerSession.Close()
	// init generated functions
	getWeather = func(ctx context.Context, input GetWeatherInput) (GetWeatherOutput, error) {
		return callMcpTool[GetWeatherInput, GetWeatherOutput](ctx, exampleStdioServerSession, "get_weather", input)
	}
	
	// Generated LLM code gets inlined here
	// ...
}
```

### Configuration

"Code Mode" is controlled via the configuration file:
```yaml
defaults:
  # Global default
  codeMode:
    enabled: true
    excludedTools:
      - some_tool
      - another_tool

models:
  - ref: sonnet
    # Global default
    codeMode:
      enabled: true
      excludedTools:
        - some_tool
        - another_tool
  - ref: small-model
    # Global default
    codeMode:
      enabled: true
      excludedTools:
        - some_tool
        - another_tool
        - another_tool_2
```

Note that "code mode" config has the option to exclude specific tools from being called within the Go code. In this case, they are registered with LLMs as regular tools. There is multiple reasons this is desired:
- This allows special tools from MCP servers that return multimedia content like images
- Some models have been trained excusively to use tools, or maybe finetuned on some built-in tools. In this case, it is beneficial to expose this tool as a regular tool to the LLM, rather than through code mode
- Some MCP servers take some time to register and set up a client session. Code mode connects to MCP servers on every tool call to `execute_go_code`, which means these servers that take time to connect will actually increase latency. In this case, we will register with the MCP server regularly, which is a long-lived connection as compared to what the `execute_go_code` tool does, requiring only a signle init and connect for client session.