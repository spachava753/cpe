package plugins_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"pgregory.net/rapid"

	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/skills"
)

const (
	manifestSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	mcpSchema      = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
)

// Domain values are the oracle. JSON/YAML encoding is fixture infrastructure;
// neither the oracle nor the generators call the production parsers.
func TestLoadValidPlugins(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		fsys := make(fstest.MapFS)
		want := make(map[string]plugins.Plugin)
		for i := range rapid.IntRange(0, 3).Draw(t, "plugin count") {
			fixture := pluginGenerator().Draw(t, "plugin")
			directory := "package-" + strconv.Itoa(i) // Not the manifest name.
			fixture.want.Path = directory
			for j := range fixture.want.Skills {
				fixture.want.Skills[j].Path = directory + "/" + fixture.want.Skills[j].Path
			}
			for name, file := range fixture.files {
				fsys[directory+"/"+name] = file
			}
			// Alternate manifests, unsupported components, and nested skills
			// are irrelevant to fixed-location discovery.
			fsys[directory+"/.claude-plugin/plugin.json"] = &fstest.MapFile{Data: []byte("invalid")}
			fsys[directory+"/commands/broken.json"] = &fstest.MapFile{Data: []byte("invalid")}
			fsys[directory+"/skills/review/references/nested/SKILL.md"] = &fstest.MapFile{Data: []byte("invalid")}
			want[directory] = fixture.want
		}
		fsys["README.md"] = &fstest.MapFile{Data: []byte("not a plugin")}

		got, err := plugins.Load(fsys)
		if err != nil || len(got) != len(want) {
			t.Fatalf("Load = %#v, %v; want %d plugins", got, err, len(want))
		}
		seen := make(map[string]bool)
		for _, actual := range got {
			expected, exists := want[actual.Path]
			if !exists || seen[actual.Path] || actual.Name != expected.Name || len(actual.Diagnostics) != 0 {
				t.Fatalf("unexpected plugin: %#v", actual)
			}
			seen[actual.Path] = true
			if !cmp.Equal(actual.MCPConfig, expected.MCPConfig, cmpopts.EquateEmpty()) || !cmp.Equal(actual.Skills, expected.Skills, cmpopts.EquateEmpty()) {
				t.Fatalf("plugin %q = %#v, want %#v", actual.Path, actual, expected)
			}
		}
	})
}

func TestLoadInvalidManifests(t *testing.T) {
	for _, change := range []struct {
		name, field string
		value       any
		omit        bool
		text        string
	}{
		{name: "missing schema", field: "$schema", omit: true},
		{name: "unsupported schema", field: "$schema", value: "https://agent-plugins.org/schemas/9.0.0/plugin.schema.json"},
		{name: "schema type", field: "$schema", value: 1},
		{name: "missing name", field: "name", omit: true},
		{name: "null name", field: "name"},
		{name: "numeric name", field: "name", value: 123},
		{name: "empty name", field: "name", value: ""},
		{name: "uppercase name", field: "name", value: "Example"},
		{name: "Unicode name", field: "name", value: "é"},
		{name: "leading hyphen", field: "name", value: "-a"},
		{name: "trailing period", field: "name", value: "a."},
		{name: "consecutive hyphens", field: "name", value: "a--b"},
		{name: "consecutive periods", field: "name", value: "a..b"},
		{name: "path separator", field: "name", value: "a/b"},
		{name: "65 characters", field: "name", value: strings.Repeat("a", 65)},
		{name: "version type", field: "version", value: 1},
		{name: "description type", field: "description", value: false},
		{name: "homepage null", field: "homepage"},
		{name: "repository type", field: "repository", value: []any{}},
		{name: "license type", field: "license", value: map[string]any{}},
		{name: "keywords null", field: "keywords"},
		{name: "keyword type", field: "keywords", value: []any{"good", 1}},
		{name: "author null", field: "author"},
		{name: "author unknown field", field: "author", value: map[string]any{"unknown": "value"}},
		{name: "author field type", field: "author", value: map[string]any{"email": false}},
		{name: "non-object", text: "[]"},
		{name: "malformed JSON", text: "{"},
		{name: "trailing data", text: `{"$schema":"` + manifestSchema + `","name":"example"} {}`},
		{name: "duplicate name", text: `{"$schema":"` + manifestSchema + `","name":"example","name":"other"}`},
	} {
		t.Run(change.name, rapid.MakeCheck(func(t *rapid.T) {
			name := pluginNameGenerator().Draw(t, "valid name")
			manifest := map[string]any{"$schema": manifestSchema, "name": name}
			if change.field != "" {
				if change.omit {
					delete(manifest, change.field)
				} else {
					manifest[change.field] = change.value
				}
			}
			file := jsonFixture(manifest)
			if change.text != "" {
				file.Data = []byte(change.text)
			}
			fsys := &observedPluginFS{ReadDirFS: fstest.MapFS{
				"a/plugin.json":            file,
				"a/skills/review/SKILL.md": skillFixture("review", "valid"),
				"a/mcp.json":               jsonFixture(map[string]any{"$schema": mcpSchema, "mcpServers": map[string]any{}}),
				"b/plugin.json":            jsonFixture(map[string]any{"$schema": manifestSchema, "name": "later"}),
			}}
			got, err := plugins.Load(fsys)
			if err == nil || len(got) != 0 {
				t.Fatalf("accepted invalid manifest %s: %#v, %v", change.name, got, err)
			}
			for _, opened := range fsys.opened {
				if opened == "a/mcp.json" || strings.HasPrefix(opened, "a/skills/") || opened == "b/plugin.json" {
					t.Fatalf("discovered components after invalid manifest: %v", fsys.opened)
				}
			}
		}))
	}
}

func TestLoadComponentIsolation(t *testing.T) {
	for _, change := range []struct {
		name, field string
		value       any
		manifest    bool
		wholeMCP    string
		skill       bool
	}{
		{name: "unknown manifest field", manifest: true, field: "skills", value: "./alternate"},
		{name: "non-object extensions", manifest: true, field: "extensions", value: 42},
		{name: "unimplemented extension contents", manifest: true, field: "extensions", value: map[string]any{"com.example.unknown": []any{nil, 42}}},
		{name: "invalid skill", skill: true},
		{name: "malformed MCP document", wholeMCP: "{"},
		{name: "unsupported MCP schema", wholeMCP: `{"$schema":"https://agent-plugins.org/schemas/9.0.0/mcp.schema.json","mcpServers":{}}`},
		{name: "missing MCP schema", wholeMCP: `{"mcpServers":{}}`},
		{name: "MCP top-level unknown field", wholeMCP: `{"$schema":"` + mcpSchema + `","mcpServers":{},"other":true}`},
		{name: "MCP servers null", wholeMCP: `{"$schema":"` + mcpSchema + `","mcpServers":null}`},
		{name: "unknown transport", field: "type", value: "other"},
		{name: "numeric transport", field: "type", value: 1},
		{name: "null transport", field: "type"},
		{name: "empty command", field: "command", value: ""},
		{name: "null command", field: "command"},
		{name: "dot command", field: "command", value: "."},
		{name: "parent command", field: "command", value: ".."},
		{name: "shell command", field: "command", value: "echo hello"},
		{name: "absolute command", field: "command", value: "/bin/echo"},
		{name: "escaping command", field: "command", value: "./../../outside"},
		{name: "command placeholder", field: "command", value: "${PLUGIN_ROOT}/bin/server"},
		{name: "argument type", field: "args", value: []any{"ok", 1}},
		{name: "null argument element", field: "args", value: []any{nil}},
		{name: "arguments null", field: "args"},
		{name: "environment type", field: "env", value: map[string]any{"KEY": false}},
		{name: "null environment", field: "env"},
		{name: "reserved root variable", field: "env", value: map[string]any{"PLUGIN_ROOT": "override"}},
		{name: "reserved data variable", field: "env", value: map[string]any{"PLUGIN_DATA": "override"}},
		{name: "bare cwd", field: "cwd", value: "data"},
		{name: "escaping cwd", field: "cwd", value: "${PLUGIN_ROOT}/../outside"},
		{name: "escaping data cwd", field: "cwd", value: "${PLUGIN_DATA}/../../outside"},
		{name: "cwd null", field: "cwd"},
		{name: "wrong variant field", field: "url", value: "https://example.com/mcp"},
		{name: "unknown server field", field: "other", value: true},
	} {
		t.Run(change.name, rapid.MakeCheck(func(t *rapid.T) {
			fixture := pluginGenerator().Draw(t, "valid plugin")
			manifest := map[string]any{"$schema": manifestSchema, "name": fixture.want.Name}
			if change.manifest {
				manifest[change.field] = change.value
			}
			fixture.files["plugin.json"] = jsonFixture(manifest)
			if change.skill {
				fixture.files["skills/bad/SKILL.md"] = &fstest.MapFile{Data: []byte("invalid")}
			} else if change.wholeMCP != "" {
				fixture.files["mcp.json"] = &fstest.MapFile{Data: []byte(change.wholeMCP)}
				fixture.want.MCPConfig = nil
			} else if !change.manifest {
				var config map[string]any
				if err := json.Unmarshal(fixture.files["mcp.json"].Data, &config); err != nil {
					t.Fatal(err)
				}
				servers := config["mcpServers"].(map[string]any)
				bad := map[string]any{"type": "stdio", "command": "unavailable-but-valid-executable"}
				bad[change.field] = change.value
				servers["bad"] = bad
				fixture.files["mcp.json"] = jsonFixture(config)
			}
			fsys := make(fstest.MapFS)
			for name, file := range fixture.files {
				fsys["bundle/"+name] = file
			}
			got, err := plugins.Load(fsys)
			if err != nil || len(got) != 1 || got[0].Name != fixture.want.Name {
				t.Fatalf("component failure rejected plugin: %#v, %v", got, err)
			}
			actual := got[0]
			for i := range fixture.want.Skills {
				fixture.want.Skills[i].Path = "bundle/" + fixture.want.Skills[i].Path
			}
			if !cmp.Equal(actual.Skills, fixture.want.Skills, cmpopts.EquateEmpty()) || !cmp.Equal(actual.MCPConfig, fixture.want.MCPConfig, cmpopts.EquateEmpty()) {
				t.Fatalf("%s damaged valid siblings: %#v, want %#v", change.name, actual, fixture.want)
			}
			if change.name == "unimplemented extension contents" {
				if len(actual.Diagnostics) != 0 {
					t.Fatalf("validated unsupported extension: %v", actual.Diagnostics)
				}
			} else if len(actual.Diagnostics) == 0 {
				t.Fatalf("%s was not reported", change.name)
			}
		}))
	}
}

func TestLoadRemoteServers(t *testing.T) {
	for _, test := range []struct {
		name, endpoint string
		headers        map[string]any
		valid          bool
	}{
		{name: "HTTPS", endpoint: "https://example.com/mcp", valid: true},
		{name: "localhost HTTP", endpoint: "http://localhost/mcp", valid: true},
		{name: "IPv4 loopback", endpoint: "http://127.3.2.1:8000/mcp", valid: true},
		{name: "IPv6 loopback", endpoint: "http://[::1]:8000/mcp", valid: true},
		{name: "mapped IPv4 loopback", endpoint: "http://[::ffff:127.0.0.1]:8000/mcp", valid: true},
		{name: "literal placeholders", endpoint: "https://example.com/${PLUGIN_ROOT}", headers: map[string]any{"X-Test": "${PLUGIN_DATA}"}, valid: true},
		{name: "public HTTP", endpoint: "http://example.com/mcp"},
		{name: "localhost suffix", endpoint: "http://localhost.example.com/mcp"},
		{name: "private IP HTTP", endpoint: "http://192.168.1.1/mcp"},
		{name: "relative URL", endpoint: "/mcp"},
		{name: "wrong scheme", endpoint: "file:///mcp"},
		{name: "user information", endpoint: "https://user:pass@example.com/mcp"},
		{name: "fragment", endpoint: "https://example.com/mcp#fragment"},
		{name: "empty fragment", endpoint: "https://example.com/mcp#"},
		{name: "invalid port", endpoint: "https://example.com:not-a-port/mcp"},
		{name: "header case collision", endpoint: "https://example.com", headers: map[string]any{"X-Test": "one", "x-test": "two"}},
		{name: "invalid header name", endpoint: "https://example.com", headers: map[string]any{"Bad Name": "value"}},
		{name: "invalid header value", endpoint: "https://example.com", headers: map[string]any{"X-Test": "a\r\nb"}},
		{name: "header type", endpoint: "https://example.com", headers: map[string]any{"X-Test": 1}},
	} {
		t.Run(test.name, rapid.MakeCheck(func(t *rapid.T) {
			transport := rapid.SampledFrom([]string{"streamable-http", "sse"}).Draw(t, "transport")
			server := map[string]any{"type": transport, "url": test.endpoint}
			if test.headers != nil {
				server["headers"] = test.headers
			}
			fsys := fstest.MapFS{
				"bundle/plugin.json": jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"}),
				"bundle/mcp.json":    jsonFixture(map[string]any{"$schema": mcpSchema, "mcpServers": map[string]any{"remote": server}}),
			}
			got, err := plugins.Load(fsys)
			if err != nil || len(got) != 1 {
				t.Fatalf("Load = %#v, %v", got, err)
			}
			_, exists := got[0].MCPConfig["remote"]
			if exists != test.valid || (len(got[0].Diagnostics) != 0) == test.valid {
				t.Fatalf("remote validity = %t, want %t; diagnostics: %v", exists, test.valid, got[0].Diagnostics)
			}
		}))
	}
}

func TestLoadFilesystem(t *testing.T) {
	for _, test := range []struct {
		name            string
		files           fstest.MapFS
		fault           string
		fatal           bool
		cause           error
		skills, servers int
		diagnostic      bool
	}{
		{name: "empty", files: fstest.MapFS{}},
		{name: "manifest only", files: fstest.MapFS{"bundle/plugin.json": jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"})}},
		{name: "missing manifest", files: fstest.MapFS{"bundle": {Mode: fs.ModeDir}}, fatal: true, cause: fs.ErrNotExist},
		{name: "manifest read failure", files: fstest.MapFS{"bundle/plugin.json": jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"})}, fault: "bundle/plugin.json", fatal: true, cause: fs.ErrPermission},
		{name: "root read failure", files: fstest.MapFS{}, fault: ".", fatal: true, cause: fs.ErrPermission},
		{name: "skills not directory", files: fstest.MapFS{"bundle/plugin.json": jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"}), "bundle/skills": {}}, diagnostic: true},
		{name: "MCP not regular file", files: fstest.MapFS{"bundle/plugin.json": jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"}), "bundle/mcp.json": {Mode: fs.ModeDir}}, diagnostic: true},
		{name: "not a discovered skill", files: fstest.MapFS{"bundle/plugin.json": jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"}), "bundle/skills/notes/README.md": {}, "bundle/skills/directory/SKILL.md": {Mode: fs.ModeDir}}},
		{name: "skill read failure", files: fstest.MapFS{"bundle/plugin.json": jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"}), "bundle/skills/review/SKILL.md": skillFixture("review", "valid")}, fault: "bundle/skills/review/SKILL.md", diagnostic: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fsys := &observedPluginFS{ReadDirFS: test.files, fault: test.fault}
			got, err := plugins.Load(fsys)
			if (err != nil) != test.fatal || (test.cause != nil && !errors.Is(err, test.cause)) {
				t.Fatalf("Load error = %v, want fatal=%t cause=%v", err, test.fatal, test.cause)
			}
			if test.fatal {
				if len(got) != 0 {
					t.Fatalf("partial results: %#v", got)
				}
				return
			}
			if len(test.files) == 0 {
				if len(got) != 0 {
					t.Fatal(got)
				}
				return
			}
			if len(got) != 1 || len(got[0].Skills) != test.skills || len(got[0].MCPConfig) != test.servers || (len(got[0].Diagnostics) > 0) != test.diagnostic {
				t.Fatalf("unexpected plugin: %#v", got)
			}
		})
	}
}

func TestLoadContainment(t *testing.T) {
	for _, test := range []struct {
		name, link, target string
		fatal              bool
		skills, servers    int
		diagnostic         bool
	}{
		{name: "internal manifest link", link: "plugin.json", target: "manifest.json", skills: 1, servers: 1},
		{name: "escaping manifest", link: "plugin.json", target: "../outside.json", fatal: true},
		{name: "absolute manifest", link: "plugin.json", target: "/outside.json", fatal: true},
		{name: "escaping skills location", link: "skills", target: "../outside-skills", servers: 1, diagnostic: true},
		{name: "escaping MCP location", link: "mcp.json", target: "../outside.json", skills: 1, diagnostic: true},
		{name: "escaping skill", link: "skills/review/SKILL.md", target: "../../../outside.json", servers: 1, diagnostic: true},
		{name: "internal skill link", link: "skills/review/SKILL.md", target: "instructions.md", skills: 1, servers: 1},
		{name: "escaping command", link: "bin/server", target: "../../outside.json", skills: 1, diagnostic: true},
		{name: "internal command link", link: "bin/server", target: "real-server", skills: 1, servers: 1},
		{name: "file before parent traversal", link: "bin/server", target: "../plugin.json/../bin/real-server", skills: 1, diagnostic: true},
		{name: "symlink cycle", link: "plugin.json", target: "plugin.json", fatal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"bundle/plugin.json":                   jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"}),
				"bundle/manifest.json":                 jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"}),
				"bundle/skills/review/SKILL.md":        skillFixture("review", "valid"),
				"bundle/skills/review/instructions.md": skillFixture("review", "valid"),
				"bundle/mcp.json":                      jsonFixture(map[string]any{"$schema": mcpSchema, "mcpServers": map[string]any{"local": map[string]any{"type": "stdio", "command": "./bin/server"}}}),
				"bundle/bin/server":                    {Mode: 0o755}, "bundle/bin/real-server": {Mode: 0o755},
				"outside.json": {Data: []byte("must not read")},
			}
			if test.link == "skills" {
				delete(fsys, "bundle/skills/review/SKILL.md")
				delete(fsys, "bundle/skills/review/instructions.md")
			}
			fsys["bundle/"+test.link] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte(test.target)}
			// The outside fixture is a file, not another plugin directory.
			got, err := plugins.Load(fsys)
			if (err != nil) != test.fatal {
				t.Fatalf("Load = %#v, %v", got, err)
			}
			if test.fatal {
				if len(got) != 0 {
					t.Fatal(got)
				}
				return
			}
			if len(got) != 1 || len(got[0].Skills) != test.skills || len(got[0].MCPConfig) != test.servers || (len(got[0].Diagnostics) > 0) != test.diagnostic {
				t.Fatalf("containment result = %#v", got)
			}
		})
	}
	t.Run("generated symlink chains", rapid.MakeCheck(func(t *rapid.T) {
		contained := rapid.Bool().Draw(t, "contained")
		length := rapid.IntRange(0, 8).Draw(t, "link count")
		fsys := fstest.MapFS{
			"bundle/plugin.json":     jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"}),
			"bundle/mcp.json":        jsonFixture(map[string]any{"$schema": mcpSchema, "mcpServers": map[string]any{"local": map[string]any{"type": "stdio", "command": "./bin/server"}}}),
			"bundle/bin/real-server": {Mode: 0o755},
			"outside.json":           {Data: []byte("must not read")},
		}
		target := "real-server"
		if !contained {
			target = "../../outside.json"
		}
		for i := range length {
			link := "link-" + strconv.Itoa(i)
			fsys["bundle/bin/"+link] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte(target)}
			target = link
		}
		fsys["bundle/bin/server"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte(target)}
		observed := &observedPluginFS{ReadDirFS: fsys}
		got, err := plugins.Load(observed)
		if err != nil || len(got) != 1 {
			t.Fatalf("Load chain = %#v, %v", got, err)
		}
		_, loaded := got[0].MCPConfig["local"]
		if loaded != contained || (len(got[0].Diagnostics) == 0) != contained {
			t.Fatalf("chain containment=%t, loaded=%t, diagnostics=%v", contained, loaded, got[0].Diagnostics)
		}
		for _, name := range observed.opened {
			if name == "outside.json" {
				t.Fatalf("opened a path outside the package: %v", observed.opened)
			}
		}
	}))
}

func FuzzLoadPluginFiles(f *testing.F) {
	f.Add(jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"}).Data, []byte("{}"), []byte("invalid"))
	f.Add([]byte("{"), []byte("\xff\x00"), []byte("---"))
	for _, server := range []map[string]any{
		{"type": "stdio", "command": "unavailable-but-valid-executable", "args": []string{"${PLUGIN_DATA}", "../../opaque"}},
		{"type": "streamable-http", "url": "https://example.com/mcp", "headers": map[string]string{"X-Test": "${PLUGIN_ROOT}"}},
	} {
		f.Add(jsonFixture(map[string]any{"$schema": manifestSchema, "name": "example"}).Data,
			jsonFixture(map[string]any{"$schema": mcpSchema, "mcpServers": map[string]any{"server": server}}).Data,
			skillFixture("review", "valid").Data)
	}
	f.Fuzz(func(t *testing.T, manifest, mcp, skill []byte) {
		got, err := plugins.Load(fstest.MapFS{
			"bundle/plugin.json": {Data: manifest}, "bundle/mcp.json": {Data: mcp}, "bundle/skills/review/SKILL.md": {Data: skill},
		})
		if err != nil {
			if len(got) != 0 {
				t.Fatalf("partial results: %#v", got)
			}
			return
		}
		if len(got) != 1 || got[0].Name == "" || got[0].Path != "bundle" {
			t.Fatalf("invalid successful result: %#v", got)
		}
		for _, loaded := range got[0].Skills {
			if loaded.Path != "bundle/skills/review/SKILL.md" || loaded.Name != "review" {
				t.Fatalf("invalid skill: %#v", loaded)
			}
		}
	})
}

type pluginFixture struct {
	want  plugins.Plugin
	files fstest.MapFS
}

func pluginGenerator() *rapid.Generator[pluginFixture] {
	return rapid.Custom(func(t *rapid.T) pluginFixture {
		name := pluginNameGenerator().Draw(t, "name")
		manifest := map[string]any{
			"$schema": manifestSchema, "name": name,
			"version": "not-semver", "homepage": "not-a-url", "repository": "not-a-url", "license": "not-spdx",
			"author":      map[string]any{"email": "not-an-email", "url": "not-a-url"},
			"description": rapid.StringN(0, 32, -1).Draw(t, "description"), "keywords": []string{"one", "two"},
		}
		fixture := pluginFixture{want: plugins.Plugin{Name: name, MCPConfig: make(map[string]plugins.MCPServer)}, files: fstest.MapFS{"plugin.json": jsonFixture(manifest)}}
		for i := range rapid.IntRange(1, 3).Draw(t, "skills") {
			skillName := "review"
			if i > 0 {
				skillName = "skill-" + strconv.Itoa(i)
			}
			description := rapid.OneOf(rapid.Just("embedded --- delimiter"), rapid.StringOfN(rapid.SampledFrom([]rune("abé中🙂:#'\"\\-")), 1, 32, -1)).Draw(t, "skill description")
			filename := "skills/" + skillName + "/SKILL.md"
			fixture.files[filename] = skillFixture(skillName, description)
			fixture.want.Skills = append(fixture.want.Skills, skills.Skill{Name: skillName, Description: description, Path: filename})
		}
		servers := make(map[string]any)
		for i := range rapid.IntRange(1, 3).Draw(t, "servers") {
			id := "server-" + strconv.Itoa(i)
			transport := rapid.SampledFrom([]plugins.Transport{plugins.Stdio, plugins.StreamableHTTP, plugins.SSE}).Draw(t, "transport")
			server := plugins.MCPServer{Type: transport}
			wire := map[string]any{"type": string(transport)}
			if transport == plugins.Stdio {
				server.Command = rapid.SampledFrom([]string{"unavailable-but-valid-executable", "./bin/server"}).Draw(t, "command")
				server.Cwd = "${PLUGIN_ROOT}"
				wire["command"] = server.Command
				fixture.files["bin/server"] = &fstest.MapFile{Mode: 0o755, Data: []byte("not executed")}
				if rapid.Bool().Draw(t, "args") {
					server.Args = rapid.SliceOfN(rapid.SampledFrom([]string{"../../opaque", "${PLUGIN_ROOT}/file", "${PLUGIN_DATA}", "${HOME}", "--flag"}), 0, 4).Draw(t, "args values")
					wire["args"] = server.Args
				}
				if rapid.Bool().Draw(t, "env") {
					server.Env = map[string]string{"CONFIG": "${PLUGIN_ROOT}/config.json", "OPAQUE": "../../not-a-package-path"}
					wire["env"] = server.Env
				}
				if rapid.Bool().Draw(t, "cwd") {
					server.Cwd = rapid.SampledFrom([]string{"./", "${PLUGIN_ROOT}", "${PLUGIN_DATA}/cache"}).Draw(t, "cwd value")
					wire["cwd"] = server.Cwd
				}
			} else {
				endpoint := rapid.SampledFrom([]string{"https://example.com/mcp", "http://localhost:8000/mcp", "http://[::1]:8000/mcp"}).Draw(t, "endpoint")
				parsed, err := url.Parse(endpoint)
				if err != nil {
					t.Fatal(err)
				}
				server.URL = *parsed
				wire["url"] = endpoint
				if rapid.Bool().Draw(t, "headers") {
					server.Headers = http.Header{"X-Test": []string{"${PLUGIN_ROOT}"}}
					wire["headers"] = map[string]string{"x-test": "${PLUGIN_ROOT}"}
				}
			}
			servers[id] = wire
			fixture.want.MCPConfig[id] = server
		}
		fixture.files["mcp.json"] = jsonFixture(map[string]any{"$schema": mcpSchema, "mcpServers": servers})
		return fixture
	})
}

func pluginNameGenerator() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.StringMatching(`^[a-z0-9]([a-z0-9.-]{0,62}[a-z0-9])?$`).Filter(func(name string) bool { return !strings.Contains(name, "--") && !strings.Contains(name, "..") }),
		rapid.StringOfN(rapid.SampledFrom([]rune("a19")), 63, 64, -1),
	)
}

func jsonFixture(value any) *fstest.MapFile {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return &fstest.MapFile{Data: data}
}

func skillFixture(name, description string) *fstest.MapFile {
	data, err := json.Marshal(description)
	if err != nil {
		panic(err)
	}
	return &fstest.MapFile{Data: []byte("---\nname: " + name + "\ndescription: " + string(data) + "\n---\n")}
}

type observedPluginFS struct {
	fs.ReadDirFS
	fault  string
	opened []string
}

func (root *observedPluginFS) Open(name string) (fs.File, error) {
	root.opened = append(root.opened, name)
	if name == root.fault {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return root.ReadDirFS.Open(name)
}

func (root *observedPluginFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == root.fault {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrPermission}
	}
	return root.ReadDirFS.ReadDir(name)
}

func (root *observedPluginFS) ReadLink(name string) (string, error) {
	reader, ok := root.ReadDirFS.(fs.ReadLinkFS)
	if !ok {
		return "", fs.ErrPermission
	}
	return reader.ReadLink(name)
}

func (root *observedPluginFS) Lstat(name string) (fs.FileInfo, error) {
	return fs.Lstat(root.ReadDirFS, name)
}
