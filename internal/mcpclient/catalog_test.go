package mcpclient

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerPrefix(t *testing.T) {
	for _, test := range []struct {
		id   ServerID
		want string
	}{
		{
			id:   "github",
			want: "github",
		},
		{
			id:   "github.com",
			want: "github_com",
		},
		{
			id:   "github-workflow:github",
			want: "github_workflow_github",
		},
		{
			id:   "a__b",
			want: "a_b",
		},
		{
			id:   "trailing-",
			want: "trailing",
		},
		{
			id:   "1password",
			want: "_1password",
		},
		{
			id:   "---",
			want: "server",
		},
	} {
		t.Run(
			string(test.id),
			func(t *testing.T) {
				if got := serverPrefix(test.id); got != test.want {
					t.Fatalf(
						"serverPrefix(%q) = %q, want %q",
						test.id,
						got,
						test.want,
					)
				}
			},
		)
	}
}

func TestPrefixesSuffixCollisionsDeterministically(t *testing.T) {
	specs := []ServerSpec{
		stdioSpec(
			"my-server",
			"x",
		),
		stdioSpec(
			"my.server",
			"y",
		),
		stdioSpec(
			"other",
			"z",
		),
	}
	prefixes, err := Prefixes(specs)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := Prefixes([]ServerSpec{
		specs[2],
		specs[1],
		specs[0],
	})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(
		prefixes,
		reversed,
	); diff != "" {
		t.Fatalf(
			"prefixes depend on order (-first +reversed):\n%s",
			diff,
		)
	}
	want := map[ServerID]string{
		"my-server": "my_server_" + hashSuffix("my-server"),
		"my.server": "my_server_" + hashSuffix("my.server"),
		"other":     "other",
	}
	if diff := cmp.Diff(
		want,
		prefixes,
	); diff != "" {
		t.Fatalf(
			"prefixes (-want +got):\n%s",
			diff,
		)
	}
}

func TestCatalogNamesTools(t *testing.T) {
	accounts := &Server{
		Spec: stdioSpec(
			"accounts",
			"accounts",
		),
		Tools: []*mcp.Tool{
			{
				Name: "get-user",
			},
			{
				Name: "get_user",
			},
			{
				Name: "list.users",
			},
		},
	}
	other := &Server{
		Spec: stdioSpec(
			"z-other",
			"other",
		),
		Tools: []*mcp.Tool{
			{
				Name: "get-user",
			},
		},
	}
	catalog, err := NewCatalog(
		map[ServerID]string{
			"accounts": "accounts",
			"z-other":  "z_other",
		},
		[]*Server{
			other,
			accounts,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, server := range catalog.Servers() {
		for _, tool := range server.Tools {
			names = append(
				names,
				tool.Name,
			)
		}
	}
	want := []string{
		"accounts__get_user__" + hashSuffix(
			"accounts",
			"get-user",
		),
		"accounts__get_user__" + hashSuffix(
			"accounts",
			"get_user",
		),
		"accounts__list_users",
		"z_other__get_user",
	}
	// Tools within a server are sorted by name; servers are sorted by prefix.
	if !strings.HasPrefix(
		names[0],
		"accounts__get_user__",
	) {
		t.Fatalf(
			"names = %v",
			names,
		)
	}
	if diff := cmp.Diff(
		sortedCopy(want),
		sortedCopy(names),
	); diff != "" {
		t.Fatalf(
			"tool names (-want +got):\n%s",
			diff,
		)
	}
	tool, ok := catalog.Tool("accounts__list_users")
	if !ok || tool.Tool.Name != "list.users" || tool.Server.Prefix != "accounts" {
		t.Fatalf(
			"Tool lookup = %+v, %t",
			tool,
			ok,
		)
	}
	if _, ok := catalog.Server("z_other"); !ok {
		t.Fatal("server lookup by prefix failed")
	}
}

func sortedCopy(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return result
}

func TestMergeSpecsRejectsClashesAcrossSources(t *testing.T) {
	settings := stdioSpec(
		"github",
		"a",
	)
	client := stdioSpec(
		"github",
		"b",
	)
	client.Source = SourceClient
	_, err := MergeSpecs(
		[]ServerSpec{
			settings,
		},
		[]ServerSpec{
			client,
		},
	)
	if err == nil || !strings.Contains(
		err.Error(),
		`"github" is defined by both settings and ACP client`,
	) {
		t.Fatalf(
			"MergeSpecs error = %v",
			err,
		)
	}
	merged, err := MergeSpecs(
		[]ServerSpec{
			stdioSpec(
				"b",
				"b",
			),
		},
		[]ServerSpec{
			stdioSpec(
				"a",
				"a",
			),
		},
	)
	if err != nil || merged[0].ID != "a" || merged[1].ID != "b" {
		t.Fatalf(
			"MergeSpecs = %+v, %v",
			merged,
			err,
		)
	}
}

func TestDescriptionPrecedenceAndSummary(t *testing.T) {
	server := &Server{
		Spec: ServerSpec{
			Description: "configured",
		},
		Info: mcp.Implementation{
			Description: "reported",
		},
		Instructions: "\n  first line  \nsecond line",
	}
	if got := server.Description(); got != "configured" {
		t.Fatalf(
			"configured description = %q",
			got,
		)
	}
	server.Spec.Description = ""
	if got := server.Description(); got != "reported" {
		t.Fatalf(
			"reported description = %q",
			got,
		)
	}
	server.Info.Description = ""
	if got := server.Description(); got != "first line" {
		t.Fatalf(
			"instructions description = %q",
			got,
		)
	}
	long := strings.Repeat(
		"é",
		SummaryLimit+10,
	)
	if got := Summary(long); got != strings.Repeat(
		"é",
		SummaryLimit,
	)+"…" {
		t.Fatalf(
			"long summary has %d runes",
			len([]rune(got)),
		)
	}
}

func TestFullDescriptionKeepsEveryLine(t *testing.T) {
	long := strings.Repeat(
		"a",
		SummaryLimit+20,
	)
	server := &Server{
		Info: mcp.Implementation{
			Description: "First line.\n" + long,
		},
		Instructions: "Instructions.",
	}
	if got := server.FullDescription(); got != "First line.\n"+long {
		t.Fatalf(
			"full description = %q",
			got,
		)
	}
	if got := server.Description(); got != "First line." {
		t.Fatalf(
			"prompt description = %q",
			got,
		)
	}
	server.Info.Description = ""
	if server.FullDescription() != "" || server.Description() != "Instructions." {
		t.Fatal("instructions should describe the server only in the prompt")
	}
}
