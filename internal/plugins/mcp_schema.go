package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

type mcpValidators struct {
	envelope *jsonschema.Resolved
	server   *jsonschema.Resolved
}

var mcpSchemas = sync.OnceValues(func() (mcpValidators, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(mcpSchemaJSON), &schema); err != nil {
		return mcpValidators{}, fmt.Errorf("decode embedded MCP schema: %w", err)
	}
	// The envelope must not validate server entries: one invalid server must
	// not disable its valid siblings (Agent Plugins specification §7.2.2).
	schema.Properties["mcpServers"].AdditionalProperties = nil
	envelope, err := schema.Resolve(nil)
	if err != nil {
		return mcpValidators{}, fmt.Errorf("resolve MCP envelope schema: %w", err)
	}
	server, err := (&jsonschema.Schema{
		Schema: schema.Schema,
		Ref:    "#/$defs/server",
		Defs:   schema.Defs,
	}).Resolve(nil)
	if err != nil {
		return mcpValidators{}, fmt.Errorf("resolve MCP server schema: %w", err)
	}
	return mcpValidators{envelope: envelope, server: server}, nil
})

// Callers check UTF-8, duplicate keys, and trailing data with jsonObject.
func validateMCPEnvelope(data []byte, schema *jsonschema.Resolved) error {
	var instance any
	decoder := json.NewDecoder(bytes.NewReader(data))
	// A number outside float64's range in one invalid server must not cause
	// decoding of the entire envelope to fail and disable valid siblings.
	decoder.UseNumber()
	if err := decoder.Decode(&instance); err != nil {
		return err
	}
	return schema.Validate(instance)
}

// Canonical Agent Plugins 1.0.0 MCP schema, embedded as a constant to keep it
// tracked under the repository's allowlist without fetching schemas at runtime.
// Source: https://agent-plugins.org/schemas/1.0.0/mcp.schema.json
const mcpSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
  "title": "Agent Plugins MCP Configuration",
  "description": "Machine-readable schema for mcp.json in Agent Plugins 1.0.0. The Agent Plugins specification defines additional semantic and operational requirements.",
  "type": "object",
  "properties": {
    "$schema": {
      "const": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
      "description": "Canonical identifier of the MCP configuration schema for the Agent Plugins version targeted by this document."
    },
    "mcpServers": {
      "type": "object",
      "additionalProperties": {
        "$ref": "#/$defs/server"
      }
    }
  },
  "required": ["$schema", "mcpServers"],
  "additionalProperties": false,
  "$defs": {
    "server": {
      "title": "MCP server",
      "oneOf": [
        {
          "$ref": "#/$defs/stdioServer"
        },
        {
          "$ref": "#/$defs/streamableHttpServer"
        },
        {
          "$ref": "#/$defs/sseServer"
        }
      ]
    },
    "stdioServer": {
      "title": "stdio MCP server",
      "type": "object",
      "properties": {
        "type": {
          "const": "stdio"
        },
        "command": {
          "type": "string",
          "minLength": 1,
          "description": "Executable token. Resolution rules are defined by the Agent Plugins specification."
        },
        "args": {
          "type": "array",
          "items": {
            "type": "string"
          }
        },
        "env": {
          "type": "object",
          "propertyNames": {
            "not": {
              "enum": ["PLUGIN_ROOT", "PLUGIN_DATA"]
            }
          },
          "additionalProperties": {
            "type": "string"
          }
        },
        "cwd": {
          "type": "string",
          "pattern": "^(?:\\./|\\$\\{PLUGIN_ROOT\\}(?:/|$)|\\$\\{PLUGIN_DATA\\}(?:/|$))",
          "description": "Plugin-relative, PLUGIN_ROOT-rooted, or PLUGIN_DATA-rooted working directory. Filesystem containment is validated separately."
        }
      },
      "required": ["type", "command"],
      "additionalProperties": false
    },
    "streamableHttpServer": {
      "title": "Streamable HTTP MCP server",
      "type": "object",
      "properties": {
        "type": {
          "const": "streamable-http"
        },
        "url": {
          "type": "string",
          "minLength": 1,
          "description": "MCP endpoint URL. URL semantics are defined by the Agent Plugins specification."
        },
        "headers": {
          "$ref": "#/$defs/headers"
        }
      },
      "required": ["type", "url"],
      "additionalProperties": false
    },
    "sseServer": {
      "title": "Legacy HTTP+SSE MCP server",
      "type": "object",
      "properties": {
        "type": {
          "const": "sse"
        },
        "url": {
          "type": "string",
          "minLength": 1,
          "description": "MCP endpoint URL. URL semantics are defined by the Agent Plugins specification."
        },
        "headers": {
          "$ref": "#/$defs/headers"
        }
      },
      "required": ["type", "url"],
      "additionalProperties": false
    },
    "headers": {
      "title": "HTTP headers",
      "type": "object",
      "additionalProperties": {
        "type": "string"
      }
    }
  }
}`
