package toolruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spachava753/cpe/internal/mcpclient"
)

type toolCatalog struct {
	namespaces []toolNamespace
	tools      []catalogTool
}

type toolNamespace struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Instructions string   `json:"instructions"`
	Tools        []string `json:"tools"`
}

type catalogTool struct {
	Name        string         `json:"name"`
	Namespace   string         `json:"namespace"`
	Description string         `json:"description"`
	Signature   string         `json:"signature"`
	InputSchema map[string]any `json:"input_schema"`
	server      mcpclient.ServerID
	remoteName  string
	searchText  string
}

type identifierCandidate struct {
	identity string
	name     string
}

func discoverTools(ctx context.Context, mcp MCPTools, descriptions map[mcpclient.ServerID]string) (toolCatalog, error) {
	catalog := toolCatalog{}
	servers := slices.Clone(mcp.Servers())
	slices.Sort(servers)
	var candidates []identifierCandidate
	for _, server := range servers {
		candidates = append(
			candidates,
			identifierCandidate{
				identity: string(server),
				name:     "mcp__" + starlarkIdentifier(string(server)),
			},
		)
	}
	names, err := uniqueMCPNames(candidates)
	if err != nil {
		return catalog, err
	}
	candidates = nil
	for _, server := range servers {
		tools, err := mcp.Load(
			ctx,
			server,
		)
		if err != nil {
			return catalog, fmt.Errorf(
				"discover MCP server %q: %w",
				server,
				err,
			)
		}
		namespace := toolNamespace{
			Name:         names[string(server)],
			Description:  descriptions[server],
			Instructions: mcp.Instructions(server),
			Tools:        []string{},
		}
		for _, tool := range tools {
			if tool == nil || tool.Name == "" {
				return catalog, errors.New("MCP tool must have a name")
			}
			encoded, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return catalog, err
			}
			var schema map[string]any
			if err := json.Unmarshal(
				encoded,
				&schema,
			); err != nil {
				return catalog, err
			}
			if schema == nil {
				return catalog, fmt.Errorf(
					"MCP tool %q requires an input schema object",
					tool.Name,
				)
			}
			entry := catalogTool{
				Namespace:   namespace.Name,
				Description: tool.Description,
				InputSchema: schema,
				server:      server,
				remoteName:  tool.Name,
			}
			candidates = append(
				candidates,
				identifierCandidate{
					identity: string(server) + "\x00" + tool.Name,
					name:     namespace.Name + "__" + starlarkIdentifier(tool.Name),
				},
			)
			catalog.tools = append(
				catalog.tools,
				entry,
			)
		}
		catalog.namespaces = append(
			catalog.namespaces,
			namespace,
		)
	}
	names, err = uniqueMCPNames(candidates)
	if err != nil {
		return catalog, err
	}
	for i := range catalog.tools {
		tool := &catalog.tools[i]
		tool.Name = names[string(tool.server)+"\x00"+tool.remoteName]
		tool.Signature = tool.Name + "(arguments = {})"
		for j := range catalog.namespaces {
			namespace := &catalog.namespaces[j]
			if namespace.Name != tool.Namespace {
				continue
			}
			namespace.Tools = append(
				namespace.Tools,
				tool.Name,
			)
			parts := []string{
				tool.Name,
				tool.remoteName,
				tool.Description,
				namespace.Name,
				namespace.Description,
				namespace.Instructions,
			}
			parts = appendSchemaText(
				parts,
				tool.InputSchema,
			)
			tool.searchText = strings.Join(
				parts,
				" ",
			)
			break
		}
	}
	for i := range catalog.namespaces {
		slices.Sort(catalog.namespaces[i].Tools)
	}
	sort.Slice(
		catalog.namespaces,
		func(i, j int) bool { return catalog.namespaces[i].Name < catalog.namespaces[j].Name },
	)
	sort.Slice(
		catalog.tools,
		func(i, j int) bool { return catalog.tools[i].Name < catalog.tools[j].Name },
	)
	return catalog, nil
}

func starlarkIdentifier(name string) string {
	return strings.Map(
		func(character rune) rune {
			if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' {
				return character
			}
			return '_'
		},
		name,
	)
}

// Resolve normalization collisions consistently, regardless of discovery order.
func uniqueMCPNames(candidates []identifierCandidate) (map[string]string, error) {
	counts := make(map[string]int)
	for _, candidate := range candidates {
		counts[candidate.name]++
	}
	result := make(map[string]string)
	used := make(map[string]bool)
	for _, candidate := range candidates {
		if _, exists := result[candidate.identity]; exists {
			return nil, errors.New("duplicate MCP name")
		}
		name := candidate.name
		if counts[name] > 1 {
			hash := sha256.Sum256([]byte(candidate.identity))
			name += fmt.Sprintf(
				"__%x",
				hash[:6],
			)
			if counts[name] != 0 {
				return nil, errors.New("ambiguous MCP identifier")
			}
		}
		if used[name] {
			return nil, errors.New("ambiguous MCP identifier")
		}
		used[name] = true
		result[candidate.identity] = name
	}
	return result, nil
}

func (c toolCatalog) instructions() string {
	if len(c.namespaces) == 0 {
		return ""
	}
	var text strings.Builder
	text.WriteString("\n\nMCP namespaces: tools are callable Starlark builtins. Use search_tools, describe_tool, and describe_namespace to discover them.\n")
	for _, namespace := range c.namespaces {
		summary := strings.TrimSpace(namespace.Description)
		if summary == "" {
			summary = strings.TrimSpace(namespace.Instructions)
		}
		summary, _, _ = strings.Cut(
			summary,
			"\n",
		)
		fmt.Fprintf(
			&text,
			"- %s: %s\n",
			namespace.Name,
			strings.TrimSpace(summary),
		)
	}
	return text.String()
}
