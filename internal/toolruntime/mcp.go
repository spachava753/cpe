package toolruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	starjson "github.com/spachava753/starlarkx/lib/json"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spachava753/starlarkx/starlarkstruct"

	"github.com/spachava753/cpe/internal/mcpclient"
)

// Field names shared by the structs returned to scripts.
const (
	fieldName        = "name"
	fieldDescription = "description"
)

func callMCP(thread *starlark.Thread, tool *mcpclient.CatalogTool, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	current, err := currentInvocation(thread)
	if err != nil {
		return nil, err
	}
	var input starlark.Value
	switch {
	case len(args) > 1:
		return nil, fmt.Errorf(
			"%s: pass the input as one dictionary or as keyword arguments",
			fn.Name(),
		)
	case len(args) == 1 && len(kwargs) != 0:
		return nil, fmt.Errorf(
			"%s: pass the input as a dictionary or as keyword arguments, not both",
			fn.Name(),
		)
	case len(args) == 1:
		if _, ok := args[0].(*starlark.Dict); !ok {
			return nil, fmt.Errorf(
				"%s: input must be a dictionary, not %s",
				fn.Name(),
				args[0].Type(),
			)
		}
		input = args[0]
	default:
		dict := starlark.NewDict(len(kwargs))
		for _, pair := range kwargs {
			if err = dict.SetKey(
				pair[0],
				pair[1],
			); err != nil {
				return nil, err
			}
		}
		input = dict
	}
	encoded, err := starlark.Call(
		thread,
		starjson.Module.Members["encode"],
		starlark.Tuple{
			input,
		},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%s: input must be JSON-compatible: %w",
			fn.Name(),
			err,
		)
	}
	text, _ := starlark.AsString(encoded)
	result, err := tool.Server.Call(
		current.ctx,
		&mcp.CallToolParams{
			Name:      tool.Tool.Name,
			Arguments: json.RawMessage(text),
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%s: call failed, so its outcome is unknown: %w",
			fn.Name(),
			err,
		)
	}
	return toolCallOutput(
		thread,
		result,
	)
}

// fromJSON converts a Go value to Starlark through JSON, so maps become dicts.
func fromJSON(thread *starlark.Thread, value any) (starlark.Value, error) {
	if value == nil {
		return starlark.None, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return starlark.Call(
		thread,
		starjson.Module.Members["decode"],
		starlark.Tuple{
			starlark.String(data),
		},
		nil,
	)
}

func optionalString(value string) starlark.Value {
	if value == "" {
		return starlark.None
	}
	return starlark.String(value)
}

func toolCallOutput(thread *starlark.Thread, result *mcp.CallToolResult) (starlark.Value, error) {
	if result == nil {
		return nil, errors.New("MCP server returned no result")
	}
	structured, err := fromJSON(
		thread,
		result.StructuredContent,
	)
	if err != nil {
		return nil, err
	}
	var meta starlark.Value = starlark.None
	if len(result.Meta) != 0 {
		meta, err = fromJSON(
			thread,
			result.Meta,
		)
		if err != nil {
			return nil, err
		}
	}
	var texts []string
	content := make(
		[]starlark.Value,
		0,
		len(result.Content),
	)
	for _, item := range result.Content {
		block, text, err := contentBlock(
			thread,
			item,
		)
		if err != nil {
			return nil, err
		}
		if text != "" {
			texts = append(
				texts,
				text,
			)
		}
		content = append(
			content,
			block,
		)
	}
	return starlarkstruct.FromStringDict(
		starlark.String("ToolCallOutput"),
		starlark.StringDict{
			"is_error":           starlark.Bool(result.IsError),
			"content":            starlark.NewList(content),
			"structured_content": structured,
			"text": starlark.String(strings.Join(
				texts,
				"\n",
			)),
			"meta": meta,
		},
	), nil
}

// contentBlock converts one MCP content block; text is non-empty for text blocks.
func contentBlock(thread *starlark.Thread, item mcp.Content) (starlark.Value, string, error) {
	fields := starlark.StringDict{}
	text := ""
	switch item := item.(type) {
	case *mcp.TextContent:
		fields["type"] = starlark.String("text")
		fields["text"] = starlark.String(item.Text)
		text = item.Text
	case *mcp.ImageContent:
		fields["type"] = starlark.String("image")
		fields["data"] = starlark.Bytes(item.Data)
		fields["mime_type"] = starlark.String(item.MIMEType)
	case *mcp.AudioContent:
		fields["type"] = starlark.String("audio")
		fields["data"] = starlark.Bytes(item.Data)
		fields["mime_type"] = starlark.String(item.MIMEType)
	case *mcp.ResourceLink:
		fields["type"] = starlark.String("resource_link")
		fields["uri"] = starlark.String(item.URI)
		fields[fieldName] = starlark.String(item.Name)
		fields["title"] = optionalString(item.Title)
		fields[fieldDescription] = optionalString(item.Description)
		fields["mime_type"] = optionalString(item.MIMEType)
		fields["size"] = starlark.None
		if item.Size != nil {
			fields["size"] = starlark.MakeInt64(*item.Size)
		}
	case *mcp.EmbeddedResource:
		fields["type"] = starlark.String("resource")
		fields["uri"] = starlark.None
		fields["mime_type"] = starlark.None
		fields["text"] = starlark.None
		fields["data"] = starlark.None
		if resource := item.Resource; resource != nil {
			fields["uri"] = starlark.String(resource.URI)
			fields["mime_type"] = optionalString(resource.MIMEType)
			if resource.Blob != nil {
				fields["data"] = starlark.Bytes(resource.Blob)
			} else {
				fields["text"] = starlark.String(resource.Text)
			}
		}
	default:
		raw, err := fromJSON(
			thread,
			item,
		)
		if err != nil {
			return nil, "", err
		}
		kind := starlark.Value(starlark.String("unknown"))
		if dict, ok := raw.(*starlark.Dict); ok {
			if value, found, _ := dict.Get(starlark.String("type")); found {
				kind = value
			}
		}
		fields["type"] = kind
		fields["raw"] = raw
	}
	return starlarkstruct.FromStringDict(
		starlark.String("ContentBlock"),
		fields,
	), text, nil
}

func (r *runtime) describeTool(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"name",
		&name,
	); err != nil {
		return nil, err
	}
	tool, ok := r.config.Catalog.Tool(strings.TrimPrefix(
		name,
		"tools.",
	))
	if !ok {
		return starlark.None, nil
	}
	input, err := fromJSON(
		thread,
		tool.Tool.InputSchema,
	)
	if err != nil {
		return nil, err
	}
	output, err := fromJSON(
		thread,
		tool.Tool.OutputSchema,
	)
	if err != nil {
		return nil, err
	}
	title := tool.Tool.Title
	annotations := starlark.StringDict{
		"read_only":   starlark.None,
		"destructive": starlark.None,
		"idempotent":  starlark.None,
		"open_world":  starlark.None,
	}
	if hints := tool.Tool.Annotations; hints != nil {
		if title == "" {
			title = hints.Title
		}
		annotations["read_only"] = starlark.Bool(hints.ReadOnlyHint)
		annotations["idempotent"] = starlark.Bool(hints.IdempotentHint)
		if hints.DestructiveHint != nil {
			annotations["destructive"] = starlark.Bool(*hints.DestructiveHint)
		}
		if hints.OpenWorldHint != nil {
			annotations["open_world"] = starlark.Bool(*hints.OpenWorldHint)
		}
	}
	return starlarkstruct.FromStringDict(
		starlark.String("ToolDescription"),
		starlark.StringDict{
			fieldName:        starlark.String(tool.Name),
			"mcp_name":       starlark.String(tool.Tool.Name),
			"server":         starlark.String(tool.Server.Prefix),
			"title":          optionalString(title),
			fieldDescription: starlark.String(tool.Tool.Description),
			"input_schema":   input,
			"output_schema":  output,
			"annotations": starlarkstruct.FromStringDict(
				starlark.String("ToolAnnotations"),
				annotations,
			),
		},
	), nil
}

func (r *runtime) describeServer(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"name",
		&name,
	); err != nil {
		return nil, err
	}
	server, ok := r.config.Catalog.Server(name)
	if !ok {
		return starlark.None, nil
	}
	tools := make(
		[]starlark.Value,
		0,
		len(server.Tools),
	)
	for _, tool := range server.Tools {
		tools = append(
			tools,
			starlark.String(tool.Name),
		)
	}
	return starlarkstruct.FromStringDict(
		starlark.String("ServerDescription"),
		starlark.StringDict{
			fieldName:         starlark.String(server.Prefix),
			"configured_name": starlark.String(server.Spec.ID),
			fieldDescription:  optionalString(server.FullDescription()),
			"instructions":    optionalString(server.Instructions),
			"tools":           starlark.NewList(tools),
		},
	), nil
}

func (r *runtime) searchTools(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var query string
	limit := 8
	var server starlark.Value = starlark.None
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"query",
		&query,
		"limit?",
		&limit,
		"server?",
		&server,
	); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, errors.New("search_tools: limit must be positive")
	}
	prefix := ""
	switch server := server.(type) {
	case starlark.NoneType:
	case starlark.String:
		prefix = string(server)
		if _, ok := r.config.Catalog.Server(prefix); !ok {
			return nil, fmt.Errorf(
				"search_tools: unknown server %q",
				prefix,
			)
		}
	default:
		return nil, fmt.Errorf(
			"search_tools: server must be a string or None, not %s",
			server.Type(),
		)
	}
	matches := r.index.search(
		query,
		limit,
		prefix,
	)
	results := make(
		[]starlark.Value,
		0,
		len(matches),
	)
	for _, tool := range matches {
		results = append(
			results,
			starlarkstruct.FromStringDict(
				starlark.String("SearchMatch"),
				starlark.StringDict{
					fieldName:        starlark.String(tool.Name),
					"server":         starlark.String(tool.Server.Prefix),
					fieldDescription: starlark.String(mcpclient.Summary(tool.Tool.Description)),
				},
			),
		)
	}
	return starlark.NewList(results), nil
}
