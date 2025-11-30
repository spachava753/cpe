package codemode

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestSchemaToGoType(t *testing.T) {
	tests := []struct {
		name       string
		schemaJSON string
		typeName   string
		want       string
		wantErr    bool
	}{
		{
			name:       "nil schema returns string alias",
			schemaJSON: "",
			typeName:   "GetWeatherOutput",
			want:       "type GetWeatherOutput = string",
		},
		{
			name: "simple object with string field",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"city": {"type": "string", "description": "The name of the city"}
				}
			}`,
			typeName: "GetWeatherInput",
			want: `type GetWeatherInput struct {
	// City The name of the city
	City string ` + "`json:\"city\"`" + `
}`,
		},
		{
			name: "object with multiple field types",
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
			want: `type TestInput struct {
	Active bool ` + "`json:\"active\"`" + `
	Count int64 ` + "`json:\"count\"`" + `
	Name string ` + "`json:\"name\"`" + `
	Price float64 ` + "`json:\"price\"`" + `
}`,
		},
		{
			name: "object with enum field",
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
			want: `type WeatherInput struct {
	// Unit Temperature unit
	// Must be one of "celsius", "fahrenheit"
	Unit string ` + "`json:\"unit\"`" + `
}`,
		},
		{
			name: "object with nullable string field",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"nickname": {"type": ["null", "string"], "description": "Optional nickname"}
				}
			}`,
			typeName: "UserInput",
			want: `type UserInput struct {
	// Nickname Optional nickname
	Nickname *string ` + "`json:\"nickname\"`" + `
}`,
		},
		{
			name: "object with array of strings",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"tags": {"type": "array", "items": {"type": "string"}}
				}
			}`,
			typeName: "TagInput",
			want: `type TagInput struct {
	Tags []string ` + "`json:\"tags\"`" + `
}`,
		},
		{
			name: "object with array of integers",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"scores": {"type": "array", "items": {"type": "integer"}}
				}
			}`,
			typeName: "ScoreInput",
			want: `type ScoreInput struct {
	Scores []int64 ` + "`json:\"scores\"`" + `
}`,
		},
		{
			name: "nested object generates separate type",
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
			want: `type AddressInput_Location struct {
	City string ` + "`json:\"city\"`" + `
	Country string ` + "`json:\"country\"`" + `
}

type AddressInput struct {
	Location AddressInput_Location ` + "`json:\"location\"`" + `
}`,
		},
		{
			name: "array of objects generates item type",
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
			want: `type ListUsersOutput_UsersItem struct {
	Id int64 ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\"`" + `
}

type ListUsersOutput struct {
	Users []ListUsersOutput_UsersItem ` + "`json:\"users\"`" + `
}`,
		},
		{
			name: "object without properties returns map type alias",
			schemaJSON: `{
				"type": "object"
			}`,
			typeName: "EmptyInput",
			want:     "type EmptyInput = map[string]any",
		},
		{
			name:       "empty schema object returns any type alias",
			schemaJSON: `{}`,
			typeName:   "EmptySchema",
			want:       "type EmptySchema = any",
		},
		{
			name: "object with description",
			schemaJSON: `{
				"type": "object",
				"description": "Weather data input parameters",
				"properties": {
					"city": {"type": "string"}
				}
			}`,
			typeName: "WeatherInput",
			want: `// WeatherInput Weather data input parameters
type WeatherInput struct {
	City string ` + "`json:\"city\"`" + `
}`,
		},
		{
			name: "deeply nested objects",
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
			want: `type DeepInput_Outer_Inner struct {
	Value string ` + "`json:\"value\"`" + `
}

type DeepInput_Outer struct {
	Inner DeepInput_Outer_Inner ` + "`json:\"inner\"`" + `
}

type DeepInput struct {
	Outer DeepInput_Outer ` + "`json:\"outer\"`" + `
}`,
		},
		{
			name: "nullable nested object",
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
			want: `type DataInput_Metadata struct {
	Key string ` + "`json:\"key\"`" + `
}

type DataInput struct {
	Metadata *DataInput_Metadata ` + "`json:\"metadata\"`" + `
}`,
		},
		{
			name: "array without items schema",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"data": {"type": "array"}
				}
			}`,
			typeName: "ArrayInput",
			want: `type ArrayInput struct {
	Data []any ` + "`json:\"data\"`" + `
}`,
		},
		{
			name: "field with snake_case name converts to PascalCase",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"user_name": {"type": "string"},
					"created_at": {"type": "string"}
				}
			}`,
			typeName: "UserData",
			want: `type UserData struct {
	CreatedAt string ` + "`json:\"created_at\"`" + `
	UserName string ` + "`json:\"user_name\"`" + `
}`,
		},
		{
			name: "mixed nullable and non-nullable types",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"required_field": {"type": "string"},
					"optional_field": {"type": ["null", "string"]},
					"optional_number": {"type": ["null", "number"]}
				}
			}`,
			typeName: "MixedInput",
			want: `type MixedInput struct {
	OptionalField *string ` + "`json:\"optional_field\"`" + `
	OptionalNumber *float64 ` + "`json:\"optional_number\"`" + `
	RequiredField string ` + "`json:\"required_field\"`" + `
}`,
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
			want: `type GetWeatherInput struct {
	// City The name of the city to get weather for
	City string ` + "`json:\"city\"`" + `
	// Unit Temperature unit for the weather response
	// Must be one of "celsius", "fahrenheit"
	Unit string ` + "`json:\"unit\"`" + `
}`,
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
			want: `type GetWeatherOutput struct {
	// Temperature Temperature in celsius
	Temperature float64 ` + "`json:\"temperature\"`" + `
}`,
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

			if got != tt.want {
				t.Errorf("SchemaToGoType() mismatch\ngot:\n%s\n\nwant:\n%s", got, tt.want)
			}
		})
	}
}
