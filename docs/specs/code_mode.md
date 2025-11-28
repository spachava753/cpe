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
    "code": "city, err := GetCity(ctx)\nif err != nil {\n\t\treturn err\n}\n\ntemp, err := GetWeather(ctx, GetWeatherInput{\n\t\tCity: city,\n\t\tUnit: \"fahrenheit\"\n})\nif err != nil {\n\t\treturn err\n}\n\nfmt.Printf(\"Temperature: %f, City: %s\\n\", temp, city)",
    "imports": ["fmt"]
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
        "description": "The code to execute"
      },
      "imports": {
        "type": "array",
        "description": "packages that the code uses",
        "items": {
          "type": "string"
        }
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
Execute generated Golang code. The version of Go is [PLACEHOLDER]. The generated code will be inlined into a function with signature of `Run() err`. As such, you cannot define functions normally, but must assign them to a variables like so:

```go
add := func(a, b int) int {
	return a + b
}
fmt.Println(add(1, 2))
```

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

Note that the generated code will get inlined inside of a "shell" go program with the following shape:
```go
package main

// imports go here

// generated types and function definitions
// ...

func Run(ctx context.Context) error {
	// setup ...
	
	// [GENERATED CODE GOES HERE]
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
```

The error, if not nil, returned from the `Run` function, will be present in the tool result.

Any packages you use in the code, you should include in the "imports" parameter. You may also import packages with aliases or `_` for side effects e.g. `pkg2 github.com/example/pkg`, `_ github.com/example/pkg`
````

### Function Generation & Type Mapping

At runtime (during `CreateToolCapableGenerator` initialization), CPE introspects the configured MCP tools to generate Golang structs, function definitions, which are added to the tools description. In addition, the types and full function code are prepended to the generated code that the LLM generates, as a "preamble".

**Code Preamble:**
```go
// package declration and imports

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
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: input})
	if err != nil {
		fatalExit(fmt.Errorf("error calling tool %s: %w", toolName, err))
	}
	
	if len(res.Content) != 1 {
		fatalExit(fmt.Errorf("expected number of content parts from tool %s: %d", toolName, len(res.Content)))
	}
	
	var textContent string
	
	switch c := res.Content[0].(type) {
	case *mcp.TextContent:
		textContent = c.Text
	default:
		fatalExit(fmt.Errorf("unexpected content type returned from tool %s, cannot handle multimedia execpt text", toolName))
	}
	
	var output O
	
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

func Run(ctx context.Context) err {
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
	GetWeather = func(ctx context.Context, input GetWeatherInput) (GetWeatherOutput, error) {
		return callMcpTool[GetWeatherInput, GetWeatherOutput](ctx, exampleStdioServerSession, "get_weather", input)
	}
	
	// Generated LLM code gets inlined here
	// ...
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

The generated code by the LLM will be run as a standalone, compiled go program with `go run`. However, the generated code is not run as is, but actually inlined in a "shell" on each execution:
```go
package main

import (
	"fmt"
	"os"
	"signal"
	"context"
	"syscall"
	// and other std packages
)
// generated types and function definitions
// ...

func Run(ctx context.Context) error {
	// setup ...
	
	// generated code inlined here
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
```

The generated go program will be placed in the system's temporary directory (e.g. `/tmp`), with a `go.mod`. The generated structure will look something like:
```text
/tmp/cpe-tmp-[RANDOM_SUFFIX]
├── go.mod
└── main.go
```

The `go.mod` is necessary, because the generated functions and types from MCP tools will use the `github.com/modelcontextprotocol/go-sdk/mcp` package, which is not part of the std library. In addition, the LLM may use common third party packages like `golang.org/x/sync/errgroup`. After execution (or failed compilation), the generated directory will be removed. Note that there will be a generated suffix for the temporary directory, since the `execute_go_code` tool be invoked by different CPE cli processes.

Note that on error from executing `Run(ctx context.Context) error`, we actually print the error, and exit with an **exit code of 1**. This is to differentiate from the other non-zero exit codes:
- error code of 0: successful execution, no error
- error code of 2: happens when the go program panics
- error code of 3: special exit code which is produced by the generated code. Means that something went wrong with the generated code, despite compiling successfully.

Alternatives were considered, such as the `yaegi` interpreter, or a WASM based approach using something like the Extism SDK. However, the main problem with both approaches is that the std library is not a 100% covered. In addition, other features like reflection has questionable support. Using the above approach gives us the most flexibility, as well as no "unexpected" surprises when it comes to language support.

### Error handling

There are different classes of errors that can be surfaced during the init and execution of the `execute_go_code` tool, some of which can be fed back to the LLM, and other errors must be propogated and require a program exit.
- Compilation error: if the generated code fails to compile, the error from the go compiler is should be returned as an erroneous tool result, as this is something the LLM can adapt to
- Error returned: after execution, the generated program may exit with a non-zero exit code of 1. In this case, the output should be returned as an erroneous tool result, as this is something the LLM can adapt to
- Panic: on panic, the go program will exit with a code of 2. This is also recoverable, and might be caused by a stray nil pointer referece. In this case, the output should be returned as an erroneous tool result, as this is something the LLM can adapt to
- Generated Code Error: in the special case of an exit code of 3, this means that the generated code ran into a runtime issue which should not occur, and is not recoverable. This is a critical error, and should reported to user, and CPE should stop further execution of the agent.

### Security

The generated go program will run as a subprocess, inherting the environment variables, permissions and all other configuration from the parent CPE process. The expecation is that the LLM is **trusted**, especially since the user controls the LLM system instructions, exposed tools, and code mode. It is the expectation the user will execute CPE, code mode or not, in a sandbox like a container, if there is a concern for security.

### Tool call rendering

Currently, when an LLM performs a tool call, the tool call is simply rendered as JSON, and printed in a markdown code block. When code mode is enabled, we should print the code and import generated as a `go` markdown block, which allows glamour to properly syntax highlight the code, and the user is more clearly able to follow along with the LLM's generated code. We should generate the following markdown:
````markdown
```go
imports (
	"fmt"
	"os"
	// other imports`
)
```
```go
fmt.Println("hello from generated code")
```
````

The above markdown will be rendered by `glamour`, the terminal markdown render package we use.

### Context Cancellation

The "shell" listens for a SIGTERM or SIGINTERRUPT signal, which will propogate context throughout the go program. We also want to make sure that when the parent process CPE CLI receives a signal from the user, we should also **send** a signal to the child go program that is executing for the `execute_go_code` tool, and wait for program exit, up till a point. Then we want to kill the program, and the grace period will be set to 5 seconds.

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
- Some servers keep state, in which case it is more beneficial to exclude the server's tools so the state is maintained through the entire agent execution run, rather than the lifetime of the execution of the `execute_go_code` tool
- Some models have been trained excusively to use tools, or maybe finetuned on some built-in tools. In this case, it is beneficial to expose this tool as a regular tool to the LLM, rather than through code mode
