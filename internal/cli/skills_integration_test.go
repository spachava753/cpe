package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/skills"
	acquisition "github.com/spachava753/cpe/internal/source"
)

func TestSkillsLifecycle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	t.Setenv(
		"GIT_CONFIG_NOSYSTEM",
		"1",
	)
	t.Setenv(
		"GIT_CONFIG_GLOBAL",
		os.DevNull,
	)
	repository := t.TempDir()
	definition := "---\nname: alpha\ndescription: Alpha version one\ndisable-model-invocation: true\n---\nInstructions\n"
	if err := os.MkdirAll(
		filepath.Join(
			repository,
			"alpha",
		),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(
			repository,
			"alpha",
			"SKILL.md",
		),
		[]byte(definition),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(
			repository,
			"alpha",
			"run.sh",
		),
		[]byte("echo alpha\n"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(
			repository,
			"other",
			"alpha",
		),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(
			repository,
			"other",
			"alpha",
			"SKILL.md",
		),
		[]byte("---\nname: alpha\ndescription: Other variant\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{
			"init",
			"--quiet",
			repository,
		},
		{
			"-C",
			repository,
			"add",
			".",
		},
		{
			"-C",
			repository,
			"-c",
			"user.name=Test",
			"-c",
			"user.email=test@example.com",
			"commit",
			"--quiet",
			"-m",
			"fixture",
		},
	} {
		if output, err := exec.CommandContext(
			t.Context(),
			"git",
			args...,
		).CombinedOutput(); err != nil {
			t.Fatalf(
				"git %v: %v, %s",
				args,
				err,
				output,
			)
		}
	}
	source := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(repository),
	}).String()
	destinations := afero.NewMemMapFs()
	manager := skills.NewManager(
		acquisition.GitClient{},
		destinations,
	)
	t.Run(
		"duplicate name returns actionable error without installation",
		func(t *testing.T) {
			var output bytes.Buffer
			command := Root(
				"test",
				manager,
				nil,
			)
			command.SetArgs([]string{
				"skills",
				"install",
				source,
				"--scope",
				"project",
				"--dir",
				"/skills",
				"--skill",
				"alpha",
			})
			command.SetOut(&output)
			command.SetErr(&output)
			err := command.ExecuteContext(t.Context())
			if err == nil || !strings.Contains(
				err.Error(),
				`ambiguous skill name "alpha" at source directories ["alpha" "other/alpha"]; use --path to select one`,
			) {
				t.Fatalf(
					"ambiguous installation error = %v",
					err,
				)
			}
			if _, err := destinations.Stat("/skills"); !errors.Is(
				err,
				fs.ErrNotExist,
			) || !strings.Contains(
				output.String(),
				`Error: ambiguous skill name "alpha" at source directories ["alpha" "other/alpha"]; use --path to select one`,
			) {
				t.Fatalf(
					"ambiguous install changed destination or omitted its diagnostic: %v, %q",
					err,
					&output,
				)
			}
		},
	)
	t.Run(
		"install by exact source path",
		func(t *testing.T) {
			var output bytes.Buffer
			command := Root(
				"test",
				manager,
				nil,
			)
			command.SetArgs([]string{
				"skills",
				"install",
				source,
				"--scope",
				"project",
				"--dir",
				"/skills",
				"--path",
				"alpha",
			})
			command.SetOut(&output)
			command.SetErr(&output)
			if err := command.ExecuteContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			data, err := afero.ReadFile(
				destinations,
				"/skills/alpha/SKILL.md",
			)
			var summaries []map[string]any
			if err := json.Unmarshal(
				output.Bytes(),
				&summaries,
			); err != nil {
				t.Fatal(err)
			}
			if len(summaries) != 1 || len(summaries[0]) != 3 ||
				summaries[0]["name"] != "alpha" || summaries[0]["description"] != "Alpha version one" ||
				summaries[0]["path"] != filepath.FromSlash("/skills/alpha/SKILL.md") {
				t.Fatalf(
					"automatic install JSON = %#v",
					summaries,
				)
			}
			if err != nil || string(data) != definition {
				t.Fatalf(
					"installed definition = %q, %v",
					data,
					err,
				)
			}
			info, err := destinations.Stat("/skills/alpha/run.sh")
			if err != nil || info.Mode().Perm() != 0o755 {
				t.Fatalf(
					"executable support file = %v, %v",
					info,
					err,
				)
			}
			entries, err := afero.ReadDir(
				destinations,
				"/skills",
			)
			if err != nil || len(entries) != 1 || entries[0].Name() != "alpha" {
				t.Fatalf(
					"installation wrote more than its bundle: %v, %v",
					entries,
					err,
				)
			}
		},
	)
	t.Run(
		"list",
		func(t *testing.T) {
			var output bytes.Buffer
			command := Root(
				"test",
				manager,
				nil,
			)
			command.SetArgs([]string{
				"skills",
				"list",
				"--dir",
				"/skills",
			})
			command.SetOut(&output)
			if err := command.ExecuteContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(
				output.String(),
				"alpha",
			) || !strings.Contains(
				output.String(),
				"Alpha version one",
			) {
				t.Errorf(
					"inventory = %q",
					&output,
				)
			}
		},
	)
	t.Run(
		"list JSON exposes invocation status",
		func(t *testing.T) {
			var output bytes.Buffer
			command := Root(
				"test",
				manager,
				nil,
			)
			command.SetArgs([]string{
				"skills",
				"ls",
				"--dir",
				"/skills",
				"--json",
			})
			command.SetOut(&output)
			if err := command.ExecuteContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			var entries []map[string]any
			if err := json.Unmarshal(
				output.Bytes(),
				&entries,
			); err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || len(entries[0]) != 4 || entries[0]["name"] != "alpha" ||
				entries[0]["description"] != "Alpha version one" ||
				entries[0]["path"] != filepath.FromSlash("/skills/alpha/SKILL.md") ||
				entries[0]["disable-model-invocation"] != true {
				t.Fatalf(
					"skill summary = %#v",
					entries,
				)
			}
		},
	)
	t.Run(
		"reinstall with override",
		func(t *testing.T) {
			definition = strings.ReplaceAll(
				definition,
				"version one",
				"version two",
			)
			if err := os.WriteFile(
				filepath.Join(
					repository,
					"alpha",
					"SKILL.md",
				),
				[]byte(definition),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(
				repository,
				"alpha",
				"run.sh",
			)); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{
				{
					"-C",
					repository,
					"add",
					".",
				},
				{
					"-C",
					repository,
					"-c",
					"user.name=Test",
					"-c",
					"user.email=test@example.com",
					"commit",
					"--quiet",
					"-m",
					"replace bundle",
				},
			} {
				if output, err := exec.CommandContext(
					t.Context(),
					"git",
					args...,
				).CombinedOutput(); err != nil {
					t.Fatalf(
						"git %v: %v, %s",
						args,
						err,
						output,
					)
				}
			}
			var output bytes.Buffer
			command := Root(
				"test",
				manager,
				nil,
			)
			command.SetArgs([]string{
				"skills",
				"install",
				source,
				"--scope",
				"project",
				"--dir",
				"/skills",
				"--path",
				"alpha",
				"--override",
			})
			command.SetOut(&output)
			if err := command.ExecuteContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			data, err := afero.ReadFile(
				destinations,
				"/skills/alpha/SKILL.md",
			)
			if err != nil || string(data) != definition {
				t.Fatalf(
					"replacement definition = %q, %v",
					data,
					err,
				)
			}
			if _, err := destinations.Stat("/skills/alpha/run.sh"); !errors.Is(
				err,
				fs.ErrNotExist,
			) {
				t.Fatal("obsolete supporting file remains")
			}
		},
	)
	t.Run(
		"remove",
		func(t *testing.T) {
			var output bytes.Buffer
			command := Root(
				"test",
				manager,
				nil,
			)
			command.SetArgs([]string{
				"skills",
				"remove",
				"alpha",
				"--dir",
				"/skills",
			})
			command.SetOut(&output)
			if err := command.ExecuteContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			var summaries []map[string]any
			if err := json.Unmarshal(
				output.Bytes(),
				&summaries,
			); err != nil {
				t.Fatal(err)
			}
			if len(summaries) != 1 || len(summaries[0]) != 3 ||
				summaries[0]["name"] != "alpha" || summaries[0]["description"] != "Alpha version two" ||
				summaries[0]["path"] != filepath.FromSlash("/skills/alpha/SKILL.md") {
				t.Fatalf(
					"automatic removal JSON = %#v",
					summaries,
				)
			}
			if _, err := destinations.Stat("/skills/alpha"); !errors.Is(
				err,
				fs.ErrNotExist,
			) {
				t.Errorf(
					"bundle remains: %v",
					err,
				)
			}
			entries, err := afero.ReadDir(
				destinations,
				"/skills",
			)
			if err != nil || len(entries) != 0 {
				t.Errorf(
					"removal left files behind: %v, %v",
					entries,
					err,
				)
			}
		},
	)
}
