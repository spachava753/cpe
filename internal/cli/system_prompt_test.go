package cli

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spachava753/dyson"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spf13/afero"
)

func TestSystemPromptModules(t *testing.T) {
	stdlib := dyson.NewStdlib(dyson.StdlibConfig{
		FS: afero.NewMemMapFs(),
	})
	defer func() {
		if err := stdlib.Close(); err != nil {
			t.Error(err)
		}
	}()
	var documented []string
	for _, match := range regexp.MustCompile("(?m)^- `([a-z]+\\.star)`:").FindAllStringSubmatch(
		defaultSystemPrompt,
		-1,
	) {
		documented = append(
			documented,
			match[1],
		)
	}
	slices.Sort(documented)
	available := slices.Sorted(maps.Keys(stdlib.Modules()))
	if !slices.Equal(
		documented,
		available,
	) {
		t.Fatalf(
			"prompt module inventory %v differs from installed Dyson %v",
			documented,
			available,
		)
	}
}

func TestSystemPromptExamples(t *testing.T) {
	examples := regexp.MustCompile("(?s)```python\n(.*?)\n```").FindAllStringSubmatch(
		defaultSystemPrompt,
		-1,
	)
	if len(examples) == 0 {
		t.Fatal("no runnable examples")
	}
	for _, example := range examples {
		t.Run(
			example[1],
			func(t *testing.T) {
				var output strings.Builder
				sphere := dyson.NewSphere(
					func(_ *starlark.Thread, text string) { output.WriteString(text) },
					dyson.NewStdlib(dyson.StdlibConfig{
						FS: afero.NewMemMapFs(),
					}),
				)
				defer func() {
					if err := sphere.Close(); err != nil {
						t.Error(err)
					}
				}()
				if err := sphere.Eval(
					t.Context(),
					example[1],
				); err != nil {
					t.Fatal(err)
				}
				if output.String() != "{\"ok\": True}\n" {
					t.Fatalf(
						"file/JSON example output: %q",
						output.String(),
					)
				}
			},
		)
	}
}
