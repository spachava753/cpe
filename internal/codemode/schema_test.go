package codemode

import (
	"encoding/json"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestSchemaToGoType(t *testing.T) {
	tests := []struct {
		name       string
		schemaJSON string
		typeName   string
		wantErr    bool
	}{
		{
			name:       "nil schema returns string alias",
			schemaJSON: "",
			typeName:   "GetWeatherOutput",
		},
		{
			name: "simple object with string field (optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"city": {"type": "string", "description": "The name of the city"}
				}
			}`,
			typeName: "GetWeatherInput",
		},
		{
			name: "object with multiple field types (all optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"count": {"type": "integer"},
					"price": {"type": "number"},
					"active": {"type": "boolean"}
				}
			}`,
			typeName: "TestInput",
		},
		{
			name: "object with enum field (optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"unit": {
						"type": "string",
						"enum": ["celsius", "fahrenheit"],
						"description": "Temperature unit"
					}
				}
			}`,
			typeName: "WeatherInput",
		},
		{
			name: "object with nullable string field (optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"nickname": {"type": ["null", "string"], "description": "Optional nickname"}
				}
			}`,
			typeName: "UserInput",
		},
		{
			name: "object with array of strings (optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"tags": {"type": "array", "items": {"type": "string"}}
				}
			}`,
			typeName: "TagInput",
		},
		{
			name: "object with array of integers (optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"scores": {"type": "array", "items": {"type": "integer"}}
				}
			}`,
			typeName: "ScoreInput",
		},
		{
			name: "nested object generates separate type (all optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"location": {
						"type": "object",
						"properties": {
							"city": {"type": "string"},
							"country": {"type": "string"}
						}
					}
				}
			}`,
			typeName: "AddressInput",
		},
		{
			name: "array of objects generates item type (all optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"users": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"id": {"type": "integer"},
								"name": {"type": "string"}
							}
						}
					}
				}
			}`,
			typeName: "ListUsersOutput",
		},
		{
			name: "object without properties returns map type alias",
			schemaJSON: `{
				"type": "object"
			}`,
			typeName: "EmptyInput",
		},
		{
			name:       "empty schema object returns any type alias",
			schemaJSON: `{}`,
			typeName:   "EmptySchema",
		},
		{
			name: "object with description (optional field)",
			schemaJSON: `{
				"type": "object",
				"description": "Weather data input parameters",
				"properties": {
					"city": {"type": "string"}
				}
			}`,
			typeName: "WeatherInput",
		},
		{
			name: "deeply nested objects (all optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"outer": {
						"type": "object",
						"properties": {
							"inner": {
								"type": "object",
								"properties": {
									"value": {"type": "string"}
								}
							}
						}
					}
				}
			}`,
			typeName: "DeepInput",
		},
		{
			name: "nullable nested object (optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"metadata": {
						"type": ["null", "object"],
						"properties": {
							"key": {"type": "string"}
						}
					}
				}
			}`,
			typeName: "DataInput",
		},
		{
			name: "array without items schema (optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"data": {"type": "array"}
				}
			}`,
			typeName: "ArrayInput",
		},
		{
			name: "field with snake_case name converts to PascalCase (optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"user_name": {"type": "string"},
					"created_at": {"type": "string"}
				}
			}`,
			typeName: "UserData",
		},
		{
			name: "mixed nullable and non-nullable types (all optional)",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"required_field": {"type": "string"},
					"optional_field": {"type": ["null", "string"]},
					"optional_number": {"type": ["null", "number"]}
				}
			}`,
			typeName: "MixedInput",
		},
		{
			name: "get_weather example from spec",
			schemaJSON: `{
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
			}`,
			typeName: "GetWeatherInput",
		},
		{
			name: "get_weather output example from spec",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"temperature": {
						"type": "number",
						"description": "Temperature in celsius"
					}
				},
				"required": ["temperature"]
			}`,
			typeName: "GetWeatherOutput",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var schema *jsonschema.Schema
			if tt.schemaJSON != "" {
				schema = &jsonschema.Schema{}
				if err := json.Unmarshal([]byte(tt.schemaJSON), schema); err != nil {
					t.Fatalf("failed to unmarshal test schema: %v", err)
				}
			}

			got, err := SchemaToGoType(schema, tt.typeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("SchemaToGoType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			cupaloy.SnapshotT(t, got)
		})
	}
}
