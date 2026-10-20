package toolruntime

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"
)

// mcpContentValue keeps media typed; its repr never dumps binary data.
type mcpContentValue struct {
	content mcp.Content
	raw     starlark.Value
}

func (c *mcpContentValue) String() string        { return "<MCP content; use emit(item) to expose it>" }
func (c *mcpContentValue) Type() string          { return "mcp_content" }
func (c *mcpContentValue) Freeze()               { c.raw.Freeze() }
func (c *mcpContentValue) Truth() starlark.Bool  { return starlark.True }
func (c *mcpContentValue) Hash() (uint32, error) { return 0, errors.New("MCP content is not hashable") }
func (c *mcpContentValue) AttrNames() []string {
	return []string{
		"type",
		"text",
		"data",
		"mime_type",
		"raw",
	}
}
func (c *mcpContentValue) Attr(name string) (starlark.Value, error) {
	if name == "raw" {
		return c.raw, nil
	}
	if name == "data" {
		switch item := c.content.(type) {
		case *mcp.ImageContent:
			return starlark.Bytes(item.Data), nil
		case *mcp.AudioContent:
			return starlark.Bytes(item.Data), nil
		}
	}
	if name == "mime_type" {
		name = "mimeType"
	}
	if dict, ok := c.raw.(*starlark.Dict); ok {
		value, found, err := dict.Get(starlark.String(name))
		if err != nil {
			return nil, err
		}
		if found {
			return value, nil
		}
	}
	return starlark.None, nil
}

func contentBlock(content mcp.Content) (gai.Block, error) {
	switch item := content.(type) {
	case *mcp.TextContent:
		return gai.TextBlock(item.Text), nil
	case *mcp.ImageContent:
		return imageBlock(item.Data)
	case *mcp.AudioContent:
		return gai.TextBlock("[Audio output retained in MCP result; model audio emission is not supported.]"), nil
	case *mcp.ResourceLink:
		return gai.TextBlock(fmt.Sprintf(
			"Resource: %s (%s) %s",
			item.Name,
			item.URI,
			item.Description,
		)), nil
	case *mcp.EmbeddedResource:
		if item.Resource == nil {
			return gai.Block{}, errors.New("empty embedded MCP resource")
		}
		resource := item.Resource
		if resource.Blob == nil {
			return gai.TextBlock(resource.Text), nil
		}
		if len(resource.MIMEType) >= 6 && resource.MIMEType[:6] == "image/" {
			return imageBlock(resource.Blob)
		}
		return gai.TextBlock("[Binary resource retained in MCP result: " + resource.URI + " (" + resource.MIMEType + ")]"), nil
	default:
		return gai.Block{}, errors.New("unsupported MCP content type")
	}
}

func imageBlock(data []byte) (gai.Block, error) {
	if len(data) == 0 || len(data) > maxOutputBytes*3/4 {
		return gai.Block{}, errors.New("image is empty or exceeds output limit")
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return gai.ImageBlock(
			data,
			mime,
		), nil
	default:
		return gai.Block{}, errors.New("only PNG, JPEG, GIF, and WebP images are supported")
	}
}
