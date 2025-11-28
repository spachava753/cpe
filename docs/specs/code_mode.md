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
    "required": ["city", "unit"]
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
    "code": "package main\n\nimport (\n\t\"context\"\n\t\"fmt\"\n)\n\nfunc Run(ctx context.Context) error {\n\tcity, err := GetCity(ctx)\n\tif err != nil {\n\t\treturn err\n\t}\n\n\ttemp, err := GetWeather(ctx, GetWeatherInput{\n\t\tCity: city,\n\t\tUnit: \"fahrenheit\",\n\t})\n\tif err != nil {\n\t\treturn err\n\t}\n\n\tfmt.Printf(\"Temperature: %f, City: %s\\n\", temp, city)\n\treturn nil\n}"
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

### Advanced Example: File I/O with Loops

This example demonstrates the true power of code mode's composability. Given a file `cities.txt` containing a list of cities (one per line), we want to get the weather for each city.

**cities.txt:**
```text
New York
Los Angeles
Chicago
Miami
Seattle
```

#### Normal tool call

With normal tool calling, the LLM must make a separate tool call for each city, resulting in 5 round-trips:

##### User

Get the weather for each city in cities.txt

##### Assistant

Thinking: Let me read the file first
Calling Tool:
```json
{
  "name": "read_file",
  "parameters": {
    "path": "cities.txt"
  }
}
```

##### Tool Result

```text
New York
Los Angeles
Chicago
Miami
Seattle
```

##### Assistant

Thinking: I'll get the weather for New York first
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

##### Tool Result

```json
{
  "temperature": 72
}
```

##### Assistant

Thinking: Now Los Angeles
Calling Tool:
```json
{
  "name": "get_weather",
  "parameters": {
    "city": "Los Angeles",
    "unit": "fahrenheit"
  }
}
```

##### Tool Result

```json
{
  "temperature": 85
}
```

...and so on for Chicago, Miami, and Seattle, resulting in **6 total tool calls** and **6 round-trips** to the model.

#### Code Mode

With code mode, the LLM handles everything in a single execution:

##### User

Get the weather for each city in cities.txt

##### Assistant

Thinking: I'll read the file and get weather for all cities in one go
Calling Tool:
```json
{
  "name": "execute_go_code",
  "parameters": {
    "code": "package main\n\nimport (\n\t\"bufio\"\n\t\"context\"\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n)\n\nfunc Run(ctx context.Context) error {\n\tfile, err := os.Open(\"cities.txt\")\n\tif err != nil {\n\t\treturn err\n\t}\n\tdefer file.Close()\n\n\tvar results []string\n\tscanner := bufio.NewScanner(file)\n\tfor scanner.Scan() {\n\t\tcity := strings.TrimSpace(scanner.Text())\n\t\tif city == \"\" {\n\t\t\tcontinue\n\t\t}\n\n\t\tweather, err := GetWeather(ctx, GetWeatherInput{\n\t\t\tCity: city,\n\t\t\tUnit: \"fahrenheit\",\n\t\t})\n\t\tif err != nil {\n\t\t\treturn fmt.Errorf(\"failed to get weather for %s: %w\", city, err)\n\t\t}\n\n\t\tresults = append(results, fmt.Sprintf(\"%s: %.0f°F\", city, weather.Temperature))\n\t}\n\n\tif err := scanner.Err(); err != nil {\n\t\treturn err\n\t}\n\n\tfmt.Println(strings.Join(results, \"\\n\"))\n\treturn nil\n}"
  }
}
```

##### Tool Result

```json
{
  "output": "New York: 72°F\nLos Angeles: 85°F\nChicago: 68°F\nMiami: 88°F\nSeattle: 62°F\n"
}
```

##### Assistant

Here's the weather for each city:
- New York: 72°F
- Los Angeles: 85°F
- Chicago: 68°F
- Miami: 88°F
- Seattle: 62°F

#### Why This Matters

This example highlights several advantages of code mode:

1. **Single round-trip**: 1 tool call vs 6, dramatically reducing latency
2. **Native control flow**: The `for` loop iterates over cities without model intervention
3. **Standard library access**: Direct file I/O with `os` and `bufio` packages
4. **In-code data processing**: Results are formatted and aggregated before returning
5. **Error handling**: Proper Go error handling without model re-prompting

For N cities, normal tool calling requires N+1 round-trips (read file + N weather calls), while code mode always requires exactly 1. This O(N) vs O(1) difference becomes significant as the number of items grows.

## Architecture

### The "Execute Go Code" Tool

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
        "description": "Complete Go source file contents implementing the Run function"
      },
      "executionTimeout": {
        "type": "integer",
        "description": "Maximum execution time in seconds (1-300). Estimate based on expected runtime of the generated code.",
        "minimum": 1,
        "maximum": 300
      }
    },
    "required": ["code", "executionTimeout"]
  },
  "outputSchema": {
    "type": "object",
    "properties": {
      "output": {
        "type": "string",
        "description": "Combined stdout and stderr output from the program"
      }
    },
    "required": ["output"]
  }
}
```

The description of the tool will be:
````markdown
Execute generated Golang code. The version of Go is [PLACEHOLDER]. You must generate a complete Go source file that implements the `Run(ctx context.Context) error` function. The file will be compiled alongside a `main.go` that calls your `Run` function.

Keep in mind you have access to the following functions and types when generating code:
```go
type GetWeatherInput struct {
	// City The name of the city to get weather for
	City string `json:"city"`
	// Unit Temperature unit for the weather response
	// Must be one of "fahrenheit", "celsius"
	Unit string `json:"unit"`
}

type GetWeatherOutput struct {
	// Temperature Temperature in celsius
	Temperature float64 `json:"temperature"`
}

// GetWeather Get current weather data for a location
var GetWeather func(ctx context.Context, input GetWeatherInput) (GetWeatherOutput, error)
```

Your generated code should be a complete Go file with the following structure:
```go
package main

import (
	"context"
	"fmt"
	// add other imports as needed
)

func Run(ctx context.Context) error {
	// your implementation here
	return nil
}
```

The `main.go` file (which you don't need to generate) will have the following shape:
```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	// and other std packages
)

// generated types and function definitions
// ...

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	
	// setup code that initializes the generated functions
	// ...
	
	err := Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
```

The error, if not nil, returned from the `Run` function, will be present in the tool result.

IMPORTANT: Generate the complete file contents including package declaration and imports. This ensures that any compilation errors report accurate line numbers that you can use for debugging.
````

### Function Generation & Type Mapping

At runtime (during `CreateToolCapableGenerator` initialization), CPE introspects the configured MCP tools to generate Golang structs and function definitions, which are added to the tool's description. The LLM generates a complete Go source file containing the `Run` function, which is placed in `run.go`. CPE generates `main.go` with the types, function definitions, MCP setup code, and the entry point that calls `Run`.

**Generated `main.go`:**

The `main.go` file is generated by CPE and contains the types, function definitions, MCP setup code, and the main entry point. The LLM's generated code goes into a separate file (e.g., `run.go`).

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fatalExit(err error) {
	fmt.Println(err)
	os.Exit(3)
}

// Generated types and function definitions

type GetWeatherInput struct {
	// City The name of the city to get weather for
	City string `json:"city"`
	// Unit Temperature unit for the weather response
	// Must be one of "fahrenheit", "celsius"
	Unit string `json:"unit"`
}

type GetWeatherOutput struct {
	// Temperature Temperature in celsius
	Temperature float64 `json:"temperature"`
}

// GetWeather Get current weather data for a location
var GetWeather func(ctx context.Context, input GetWeatherInput) (GetWeatherOutput, error)

// End of generated types and function definitions

// callMcpTool is a reusable utility function for calling a mcp tool
func callMcpTool[I any, O any](ctx context.Context, clientSession *mcp.ClientSession, toolName string, input I) (O, error) {
	var output O

	// Check if context is already cancelled before making the call
	if err := ctx.Err(); err != nil {
		return output, err
	}

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: input})
	if err != nil {
		// Return context cancellation/deadline errors normally
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return output, err
		}
		fatalExit(fmt.Errorf("error calling tool %s: %w", toolName, err))
	}
	
	if len(result.Content) != 1 {
		fatalExit(fmt.Errorf("expected number of content parts from tool %s: %d", toolName, len(result.Content)))
	}
	
	var textContent string
	
	switch c := result.Content[0].(type) {
	case *mcp.TextContent:
		textContent = c.Text
	default:
		fatalExit(fmt.Errorf("unexpected content type returned from tool %s, cannot handle multimedia except text", toolName))
	}
	
	if result.IsError {
		return output, errors.New(textContent)
	}
	
	outputJson := []byte(textContent)
	
	if result.StructuredContent != nil {
		structuredContent, err := json.Marshal(result.StructuredContent)
		if err != nil {
			fatalExit(fmt.Errorf("could not marshal structured content: %w", err))
		}
		outputJson = structuredContent
	}
	
	if err := json.Unmarshal(outputJson, &output); err != nil {
		fatalExit(fmt.Errorf("could not unmarshal structured content json into output for tool get_weather: %w", err))
	}
	return output, nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// init mcp client
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil)
	
	// example stdio server
	exampleStdioServerCmd := exec.Command("example-stdio-server", "arg1", "arg2")
	exampleStdioServerTransport := &mcp.CommandTransport{Command: exampleStdioServerCmd}
	exampleStdioServerSession, err := mcpClient.Connect(ctx, exampleStdioServerTransport, nil)
	if err != nil {
		panic(fmt.Sprintf("could not connect to example-stdio-server server: %s", err))
	}
	defer exampleStdioServerSession.Close()
	
	// init generated functions
	GetWeather = func(ctx context.Context, input GetWeatherInput) (GetWeatherOutput, error) {
		return callMcpTool[GetWeatherInput, GetWeatherOutput](ctx, exampleStdioServerSession, "get_weather", input)
	}
	
	// Call the LLM-generated Run function from run.go
	err = Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
```

**LLM-generated `run.go`:**

The LLM generates a complete Go source file that implements the `Run` function:

```go
package main

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
		City: city,
		Unit: "fahrenheit",
	})
	if err != nil {
		return err
	}

	fmt.Printf("Temperature: %f, City: %s\n", temp, city)
	return nil
}
```

Note that the generated types and functions definitions may counter-intuitively end up consuming _more_ tokens than the original token description and input schema. However, after some turns in the conversation, for any meaningfully long conversation, code mode will always be more token efficient in the long run by virtue of processing tool results directly in the code, without taking up valuable tokens in the context window. In addition, code mode can be disabled for short conversations or for agent setups that have a small number of independent tools, so it is entirely up to the user when code mode is enabled.

More often that not, due to the relatively recent addition of output schemas to the MCP specification, tools exposed by an MCP server does not have a output schema. In this case, we do not know the shape of the output returned by a tool, except for the fact that it is JSON object. In cases like this, the type for the output should be a `map[string]any`. So in the example above, `GetWeatherOutput` would be `type GetWeatherOutput map[string]any`.

#### Reconnection Latency

While re-connecting to servers on every execution of the `execute_go_code` tool does introduce latency, it's very **rare** that init and connect time to an MCP server outshine the benefits of code mode. Stdio and streamable servers often start up in a matter of milliseconds. On the other hand, generating a response from a model, especially from an intelligent thinking model, can often take tens of seconds. So, despite the re-connection on every execution of the `execute_go_code` tool introducing some latency, we are still saving significant time in the form of reduced turns back and forth with the model, as well as reduced input and output token generation.

In addition, users of the tool can mitigate this issue completely by utilizing an MCP gateway, which will act as a proxy and persist client sessions to tools.

### Tool schema to Go types

JSON Schema can be quite complex, and it can be difficult to map a schema to a Go type because:
- JSON schema unions like `anyOf` and `oneOf`
- conditional subschemas
- location identifiers
- validation rules
- etc.

However, many, if not most tools do not use these advanced features of JSON schema. As long as we support the following features:
- types
  - objects
  - arrays
  - null
  - boolean
  - number
  - string
- schema annotations
  - description
- validation
  - enum
  - type (single)
  - types (multiple, but only a combination of `null` and some type, like `["null", "string"]`)
we will be able to convert most tool input and output schemas to Go types.

In addition, I want to convert tool names and object field names in the JSON schema to be pascal case for function names and struct fields. For each tool, the tool name will be converted to a camel case e.g. `get_weather` -> `GetWeather`. The input and output types will be the pascal case tool name with the suffix of `Input` and `Output` e.g. `GetWeatherInput`, `GetWeatherOutput`. For enumerations, we will simply add a document comment on the field, listing out the allowed string values.  

### Tool Execution

The generated code by the LLM will be run as a standalone, compiled Go program with `go run`. The LLM generates a complete source file containing the `Run` function, which is placed in a separate file (`run.go`) alongside a CPE-generated `main.go`.

The generated Go program will be placed in the system's temporary directory (e.g. `/tmp`), with a `go.mod`. The generated structure will look something like:
```text
/tmp/cpe-tmp-[RANDOM_SUFFIX]
├── go.mod
├── main.go   # CPE-generated: types, function definitions, MCP setup, main()
└── run.go    # LLM-generated: complete file with Run(ctx) implementation
```

This separation ensures that compilation errors in the LLM-generated code report accurate line numbers, making it easier for the LLM to identify and fix issues. The `main.go` file is stable and generated by CPE, while `run.go` contains only the LLM's code.

The `go.mod` is necessary, because the generated functions and types from MCP tools will use the `github.com/modelcontextprotocol/go-sdk/mcp` package, which is not part of the std library. In addition, the LLM may use common third party packages like `golang.org/x/sync/errgroup`. After creating the temporary directory and writing the files, CPE runs `go mod tidy` to download any required modules before compilation. On initial download of packages not used before, this will introduce some latency, but follow-up tool executions with generated code will be able to re-use the module cache on the system. After execution (or failed compilation), the generated directory will be removed. Note that there will be a generated suffix for the temporary directory, since the `execute_go_code` tool be invoked by different CPE cli processes.

Note that on error from executing `Run(ctx context.Context) error`, we actually print the error, and exit with an **exit code of 1**. This is to differentiate from the other non-zero exit codes:
- error code of 0: successful execution, no error
- error code of 2: happens when the go program panics
- error code of 3: special exit code which is produced by the generated code. Means that something went wrong with the generated code, despite compiling successfully.

#### Execution Timeout

The `executionTimeout` field in the tool input is required and specifies the maximum time in seconds that the generated code is allowed to run. The LLM should estimate an appropriate timeout based on the expected runtime of its generated code. The value must be between 1 and 300 seconds. When the timeout expires, CPE sends a `SIGINT` signal to the child process, waits a grace period of 5 seconds for graceful shutdown, then sends `SIGKILL` if the process is still running. This prevents runaway processes from infinite loops or unexpectedly long operations.

Alternatives were considered, such as the `yaegi` interpreter, or a WASM based approach using something like the Extism SDK. However, the main problem with both approaches is that the std library is not a 100% covered. In addition, other features like reflection has questionable support. Using the above approach gives us the most flexibility, as well as no "unexpected" surprises when it comes to language support.

### Error handling

There are different classes of errors that can be surfaced during the init and execution of the `execute_go_code` tool, some of which can be fed back to the LLM, and other errors must be propogated and require a program exit.
- Compilation error: if the generated code fails to compile, the error from the go compiler is should be returned as an erroneous tool result, as this is something the LLM can adapt to
- Error returned: after execution, the generated program may exit with a non-zero exit code of 1. In this case, the output should be returned as an erroneous tool result, as this is something the LLM can adapt to
- Panic: on panic, the go program will exit with a code of 2. This is also recoverable, and might be caused by a stray nil pointer referece. In this case, the output should be returned as an erroneous tool result, as this is something the LLM can adapt to
- Generated Code Error: in the special case of an exit code of 3, this means that the generated code ran into a runtime issue which should not occur, and is not recoverable. This is a critical error, and should reported to user, and CPE should stop further execution of the agent.

### Naming Collisions

There are two types of naming collisions that CPE must detect at startup before agent execution begins:

1. **Reserved name collision**: A tool from an MCP server may expose a tool with the same name as `execute_go_code`. If code mode is enabled and such a collision is detected, CPE should return an error to the user.

2. **Pascal case collision**: When converting tool names to pascal case for Go function names, different tool names may produce the same identifier (e.g., `get_weather` and `get_Weather` both become `GetWeather`). CPE must check that all generated function names are unique and return an error if duplicates are found.

In both cases, the user can resolve the collision by either excluding the conflicting tool from code mode via the `excludedTools` configuration (which will expose it as a regular tool instead), or by removing the problematic MCP server entirely.

### Security

The generated go program will run as a subprocess, inherting the environment variables, permissions and all other configuration from the parent CPE process. The expecation is that the LLM is **trusted**, especially since the user controls the LLM system instructions, exposed tools, and code mode. It is the expectation the user will execute CPE, code mode or not, in a sandbox like a container, if there is a concern for security.

### Tool call rendering

Currently, when an LLM performs a tool call, the tool call is simply rendered as JSON, and printed in a markdown code block. When code mode is enabled and the **non-streaming printer** is used, we should print the generated code as a `go` markdown block, which allows glamour to properly syntax highlight the code, and the user is more clearly able to follow along with the LLM's generated code. The streaming printer will treat the `execute_go_code` tool as a normal tool call and print the JSON arguments, which is acceptable. Since the LLM generates the complete file, the non-streaming printer renders it directly:
````markdown
```go
package main

import (
	"context"
	"fmt"
)

func Run(ctx context.Context) error {
	fmt.Println("hello from generated code")
	return nil
}
```
````

The above markdown will be rendered by `glamour`, the terminal markdown render package we use.

### Context Cancellation

The "shell" listens for a SIGTERM or SIGINTERRUPT signal, which will propogate context throughout the go program. We also want to make sure that when the parent process CPE CLI receives a signal from the user, we should also **send** a signal to the child go program that is executing for the `execute_go_code` tool, and wait for program exit, up till a point. Then we want to kill the program, and the grace period will be set to 5 seconds.

### Configuration

"Code Mode" is controlled via the configuration file:
```yaml
defaults:
  # Global default for all models
  codeMode:
    enabled: true
    excludedTools:
      - some_tool
      - another_tool

models:
  - ref: sonnet
    # No codeMode specified - inherits defaults.codeMode entirely
  - ref: small-model
    # Model-level override - COMPLETELY REPLACES defaults.codeMode (no merging)
    codeMode:
      enabled: true
      excludedTools:
        - some_tool
        - another_tool
        - another_tool_2
  - ref: tiny-model
    # Disable code mode for this model only
    codeMode:
      enabled: false
```

#### Configuration Resolution

When a model is selected, the effective `codeMode` configuration is determined as follows:

1. If the model specifies `codeMode`, use it **entirely** (override behavior, not merge)
2. Otherwise, fall back to `defaults.codeMode`
3. If neither is specified, code mode is disabled

This is **override** behavior, not merging. If a model provides its own `codeMode` configuration, it completely replaces the global default rather than merging `excludedTools` lists. This design choice keeps the configuration predictable: each model's code mode settings are self-contained and explicit.

For example, if `defaults.codeMode.excludedTools` contains `["tool_a", "tool_b"]` and a model specifies `codeMode.excludedTools: ["tool_c"]`, the effective excluded tools for that model is `["tool_c"]` only—not `["tool_a", "tool_b", "tool_c"]`.

Note that "code mode" config has the option to exclude specific tools from being called within the Go code. In this case, they are registered with LLMs as regular tools. There are multiple reasons this is desired:
- This allows special tools from MCP servers that return multimedia content like images
- Some servers keep state, in which case it is more beneficial to exclude the server's tools so the state is maintained through the entire agent execution run, rather than the lifetime of the execution of the `execute_go_code` tool
- Some models have been trained excusively to use tools, or maybe finetuned on some built-in tools. In this case, it is beneficial to expose this tool as a regular tool to the LLM, rather than through code mode
