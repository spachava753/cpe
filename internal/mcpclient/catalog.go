package mcpclient

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SummaryLimit caps the one-line descriptions shown in prompts and listings.
const SummaryLimit = 500

// Catalog names a session's connected servers and tools for scripts. Names
// depend only on configuration and tool lists, never on connection order.
type Catalog struct {
	byPrefix map[string]*CatalogServer
	byName   map[string]*CatalogTool
	servers  []*CatalogServer
}

// CatalogServer is a connected server with its callable prefix.
type CatalogServer struct {
	*Server
	Prefix string
	// Tools are sorted by name.
	Tools []*CatalogTool
}

// CatalogTool is one remote tool with its name under the script's `tools` value.
type CatalogTool struct {
	Tool   *mcp.Tool
	Server *CatalogServer
	Name   string
}

// identifier replaces characters a StarlarkX identifier cannot contain.
func identifier(name string) string {
	return strings.Map(
		func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
				return r
			}
			return '_'
		},
		name,
	)
}

// serverPrefix never contains "__" or ends with "_", so the first "__" in a tool
// name always ends the server prefix.
func serverPrefix(id ServerID) string {
	normalized := identifier(string(id))
	for strings.Contains(
		normalized,
		"__",
	) {
		normalized = strings.ReplaceAll(
			normalized,
			"__",
			"_",
		)
	}
	normalized = strings.TrimRight(
		normalized,
		"_",
	)
	if normalized == "" {
		normalized = "server"
	}
	if normalized[0] >= '0' && normalized[0] <= '9' {
		normalized = "_" + normalized
	}
	return normalized
}

func hashSuffix(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(
		parts,
		"\x00",
	)))
	return hex.EncodeToString(sum[:])[:6]
}

// Prefixes assigns each configured server its callable prefix. Prefixes are
// computed over every configured server, connected or not, so a server's name
// never depends on whether another server happened to connect. Colliding
// prefixes get a suffix derived from the original, pre-normalization name.
func Prefixes(specs []ServerSpec) (map[ServerID]string, error) {
	counts := make(map[string]int)
	for _, spec := range specs {
		counts[serverPrefix(spec.ID)]++
	}
	prefixes := make(map[ServerID]string)
	used := make(map[string]ServerID)
	for _, spec := range specs {
		prefix := serverPrefix(spec.ID)
		if counts[prefix] > 1 {
			prefix += "_" + hashSuffix(string(spec.ID))
		}
		if other, exists := used[prefix]; exists {
			return nil, fmt.Errorf(
				"MCP servers %q and %q have the same callable prefix %q",
				other,
				spec.ID,
				prefix,
			)
		}
		used[prefix] = spec.ID
		prefixes[spec.ID] = prefix
	}
	return prefixes, nil
}

// NewCatalog names the tools of connected servers. prefixes must cover every server.
func NewCatalog(prefixes map[ServerID]string, servers []*Server) (*Catalog, error) {
	catalog := &Catalog{
		byPrefix: make(map[string]*CatalogServer),
		byName:   make(map[string]*CatalogTool),
	}
	for _, server := range servers {
		prefix, ok := prefixes[server.Spec.ID]
		if !ok {
			return nil, fmt.Errorf(
				"MCP server %q has no callable prefix",
				server.Spec.ID,
			)
		}
		entry := &CatalogServer{
			Server: server,
			Prefix: prefix,
		}
		counts := make(map[string]int)
		for _, tool := range server.Tools {
			counts[prefix+"__"+identifier(tool.Name)]++
		}
		for _, tool := range server.Tools {
			name := prefix + "__" + identifier(tool.Name)
			if counts[name] > 1 {
				name += "__" + hashSuffix(
					string(server.Spec.ID),
					tool.Name,
				)
			}
			if _, exists := catalog.byName[name]; exists {
				return nil, fmt.Errorf(
					"MCP tools on server %q have the same callable name %q",
					server.Spec.ID,
					name,
				)
			}
			named := &CatalogTool{
				Name:   name,
				Tool:   tool,
				Server: entry,
			}
			catalog.byName[name] = named
			entry.Tools = append(
				entry.Tools,
				named,
			)
		}
		slices.SortFunc(
			entry.Tools,
			func(a, b *CatalogTool) int {
				return strings.Compare(
					a.Name,
					b.Name,
				)
			},
		)
		catalog.byPrefix[prefix] = entry
		catalog.servers = append(
			catalog.servers,
			entry,
		)
	}
	slices.SortFunc(
		catalog.servers,
		func(a, b *CatalogServer) int {
			return strings.Compare(
				a.Prefix,
				b.Prefix,
			)
		},
	)
	return catalog, nil
}

// Servers returns connected servers sorted by prefix.
func (c *Catalog) Servers() []*CatalogServer {
	return c.servers
}

// Server finds a server by its callable prefix.
func (c *Catalog) Server(prefix string) (*CatalogServer, bool) {
	server, ok := c.byPrefix[prefix]
	return server, ok
}

// Tool finds a tool by its name under `tools`.
func (c *Catalog) Tool(name string) (*CatalogTool, bool) {
	tool, ok := c.byName[name]
	return tool, ok
}

// Description is the one description shown in prompts: the configured
// description, else the server's own description, else the first line of its
// instructions, shortened by Summary. Search reads all three separately.
func (s *Server) Description() string {
	if full := s.FullDescription(); full != "" {
		return Summary(full)
	}
	return Summary(s.Instructions)
}

// FullDescription is the configured description, else the server's own
// description, unshortened. It is empty when only instructions describe the server.
func (s *Server) FullDescription() string {
	for _, candidate := range []string{
		s.Spec.Description,
		s.Info.Description,
	} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

// Summary returns the first nonempty line of text, capped at SummaryLimit
// characters and marked with an ellipsis when cut.
func Summary(text string) string {
	for line := range strings.SplitSeq(
		text,
		"\n",
	) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) <= SummaryLimit {
			return line
		}
		return string([]rune(line)[:SummaryLimit]) + "…"
	}
	return ""
}
