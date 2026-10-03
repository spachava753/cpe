package skills_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/skills"
)

const (
	managerSource = "https://example.com/skills.git"
	alphaSkill    = "---\nname: alpha\ndescription: Alpha skill\n---\n# Alpha\n"
	betaSkill     = "---\nname: beta\ndescription: Beta skill\n---\n# Beta\n"
	alphaUpdated  = "---\nname: alpha\ndescription: Updated alpha\n---\n# Updated\n"
	alphaRecord   = `{"version":1,"skills":{"alpha":{"location":"https://example.com/skills.git","directory":"alpha"}}}`
	bothRecords   = `{"version":1,"skills":{"alpha":{"location":"https://example.com/skills.git","directory":"alpha"},"beta":{"location":"https://example.com/skills.git","directory":"beta"}}}`
)

func TestManagerListSource(t *testing.T) {
	t.Parallel()
	fetchErr := errors.New("source unavailable")
	cleanupErr := errors.New("cleanup failed")
	for _, test := range []struct {
		name                     string
		files                    fstest.MapFS
		options                  skills.DiscoveryOptions
		fetchErr, cleanupErr     error
		cancel, nilClient, nilFS bool
		want                     []skills.Skill
		wantErr                  error
		errText                  string
	}{
		{name: "empty source", files: fstest.MapFS{}},
		{name: "discovers definitions not supporting files", files: fstest.MapFS{"README.md": {Data: []byte("Overview")}, "alpha/SKILL.md": {Data: []byte(alphaSkill)}, "alpha/scripts/run.sh": {Data: []byte("echo alpha\n")}}, want: []skills.Skill{{Name: "alpha", Description: "Alpha skill", Path: "alpha/SKILL.md"}}},
		{name: "nested collection", files: fstest.MapFS{"collections/alpha/SKILL.md": {Data: []byte(alphaSkill)}}, want: []skills.Skill{{Name: "alpha", Description: "Alpha skill", Path: "collections/alpha/SKILL.md"}}},
		{name: "source root bundle", files: fstest.MapFS{"SKILL.md": {Data: []byte(alphaSkill)}}, want: []skills.Skill{{Name: "alpha", Description: "Alpha skill", Path: "SKILL.md"}}},
		{name: "deterministic order", files: fstest.MapFS{"beta/SKILL.md": {Data: []byte(betaSkill)}, "alpha/SKILL.md": {Data: []byte(alphaSkill)}}, want: []skills.Skill{{Name: "alpha", Description: "Alpha skill", Path: "alpha/SKILL.md"}, {Name: "beta", Description: "Beta skill", Path: "beta/SKILL.md"}}},
		{name: "depth boundary included", files: fstest.MapFS{"a/b/c/d/alpha/SKILL.md": {Data: []byte(alphaSkill)}}, want: []skills.Skill{{Name: "alpha", Description: "Alpha skill", Path: "a/b/c/d/alpha/SKILL.md"}}},
		{name: "depth limit", files: fstest.MapFS{"a/b/c/d/e/alpha/SKILL.md": {Data: []byte(alphaSkill)}}},
		{name: "full depth", files: fstest.MapFS{"a/b/c/d/e/alpha/SKILL.md": {Data: []byte(alphaSkill)}}, options: skills.DiscoveryOptions{FullDepth: true}, want: []skills.Skill{{Name: "alpha", Description: "Alpha skill", Path: "a/b/c/d/e/alpha/SKILL.md"}}},
		{name: "ignores git metadata", files: fstest.MapFS{".git/alpha/SKILL.md": {Data: []byte("invalid")}}},
		{name: "invalid definition", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte("invalid")}}, errText: "alpha/SKILL.md"},
		{name: "mismatched name", files: fstest.MapFS{"other/SKILL.md": {Data: []byte(alphaSkill)}}, errText: "directory"},
		{name: "definition is a symlink", files: fstest.MapFS{"alpha/SKILL.md": {Mode: fs.ModeSymlink, Data: []byte("../outside")}}, errText: "regular"},
		{name: "fetch failure", fetchErr: fetchErr, wantErr: fetchErr},
		{name: "cleanup failure", files: fstest.MapFS{}, cleanupErr: cleanupErr, wantErr: cleanupErr},
		{name: "cancelled", cancel: true, wantErr: context.Canceled},
		{name: "missing client", nilClient: true, errText: "source client"},
		{name: "missing source filesystem", nilFS: true, errText: "source filesystem"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			cleaned := false
			var client skills.SourceClient = managerSourceClient{files: test.files, err: test.fetchErr, cleanup: func() error { cleaned = true; return test.cleanupErr }}
			if test.nilClient {
				client = nil
			} else if test.nilFS {
				client = managerSourceClient{}
			}
			got, err := skills.NewManager(client, afero.NewMemMapFs()).ListSource(ctx, managerSource, test.options)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want cause %v", err, test.wantErr)
				}
			} else if test.errText != "" {
				if err == nil || !strings.Contains(err.Error(), test.errText) {
					t.Fatalf("error = %v, want %q", err, test.errText)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err != nil {
				if len(got) != 0 {
					t.Fatalf("partial results: %#v", got)
				}
			} else if diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("source skills (-want +got):\n%s", diff)
			}
			if !test.cancel && !test.nilClient && !test.nilFS && test.fetchErr == nil && !cleaned {
				t.Error("acquired source was not released")
			}
		})
	}
}

func TestManagerInstall(t *testing.T) {
	t.Parallel()
	fetchErr := errors.New("source unavailable")
	for _, test := range []struct {
		name                          string
		files, existing               fstest.MapFS
		names                         []string
		fetchErr, cleanupErr          error
		cancel, readOnly, missingRoot bool
		want                          []string
		wantErr                       error
		errText                       string
	}{
		{name: "selected bundle includes supporting files", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "alpha/scripts/run.sh": {Data: []byte("echo alpha\n")}, "beta/SKILL.md": {Data: []byte(betaSkill)}}, names: []string{"alpha"}, want: []string{"alpha"}},
		{name: "all skills", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "beta/SKILL.md": {Data: []byte(betaSkill)}}, want: []string{"alpha", "beta"}},
		{name: "nested bundle provenance", files: fstest.MapFS{"collection/alpha/SKILL.md": {Data: []byte(alphaSkill)}}, want: []string{"alpha"}},
		{name: "root bundle provenance", files: fstest.MapFS{"SKILL.md": {Data: []byte(alphaSkill)}}, want: []string{"alpha"}},
		{name: "BOM-prefixed installed skill loads", files: fstest.MapFS{"alpha/SKILL.md": {Data: append([]byte{0xef, 0xbb, 0xbf}, []byte(alphaSkill)...)}}, want: []string{"alpha"}},
		{name: "duplicate selectors are deduplicated", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, names: []string{"alpha", "alpha"}, want: []string{"alpha"}},
		{name: "creates destination root", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, missingRoot: true, want: []string{"alpha"}},
		{name: "preserves existing skill and record", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, existing: fstest.MapFS{"beta/SKILL.md": {Data: []byte(betaSkill)}, "skills.json": {Data: []byte(`{"version":1,"skills":{"beta":{"location":"other","directory":"beta"}}}`)}}, want: []string{"alpha"}},
		{name: "missing selection", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, names: []string{"missing"}, errText: "missing"},
		{name: "ambiguous names", files: fstest.MapFS{"a/alpha/SKILL.md": {Data: []byte(alphaSkill)}, "b/alpha/SKILL.md": {Data: []byte(alphaSkill)}}, errText: "ambiguous"},
		{name: "rejects existing directory", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaUpdated)}}, wantErr: fs.ErrExist},
		{name: "rejects existing file", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, existing: fstest.MapFS{"alpha": {Data: []byte("keep")}}, wantErr: fs.ErrExist},
		{name: "invalid definition", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte("invalid")}}, errText: "SKILL.md"},
		{name: "rejects payload symlink", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "alpha/escape": {Mode: fs.ModeSymlink, Data: []byte("../../outside")}}, errText: "symlink"},
		{name: "corrupt metadata", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, existing: fstest.MapFS{"skills.json": {Data: []byte("broken")}}, errText: "skills.json"},
		{name: "source failure", fetchErr: fetchErr, wantErr: fetchErr},
		{name: "cleanup failure leaves destination unchanged", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, cleanupErr: fetchErr, wantErr: fetchErr},
		{name: "read only destination", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, readOnly: true, wantErr: fs.ErrPermission},
		{name: "cancelled", cancel: true, wantErr: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := filepath.FromSlash("/skills")
			base := newManagerFilesystem(t, directory, test.existing)
			if test.missingRoot {
				if err := base.RemoveAll(directory); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotManagerFS(t, base)
			destination := base
			if test.readOnly {
				destination = afero.NewReadOnlyFs(base)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			manager := skills.NewManager(managerSourceClient{files: test.files, err: test.fetchErr, cleanup: func() error { return test.cleanupErr }}, destination)
			got, err := manager.Install(ctx, skills.InstallRequest{Source: managerSource, Directory: directory, Skills: test.names})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want cause %v", err, test.wantErr)
				}
			} else if test.errText != "" {
				if err == nil || !strings.Contains(err.Error(), test.errText) {
					t.Fatalf("error = %v, want %q", err, test.errText)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err != nil {
				if diff := cmp.Diff(before, snapshotManagerFS(t, base)); diff != "" {
					t.Fatalf("failed install changed destination:\n%s", diff)
				}
				return
			}
			wantRecords := map[string]skills.SkillSource{}
			if file, ok := test.existing["skills.json"]; ok {
				var previous struct {
					Skills map[string]skills.SkillSource `json:"skills"`
				}
				if err := json.Unmarshal(file.Data, &previous); err != nil {
					t.Fatal(err)
				}
				maps.Copy(wantRecords, previous.Skills)
			}
			var names []string
			for _, entry := range got {
				names = append(names, entry.Skill.Name)
				if entry.Directory != filepath.Join(directory, entry.Skill.Name) || entry.Source == nil || entry.Source.Location != managerSource {
					t.Fatalf("bad installation result: %#v", entry)
				}
				sourceFile := ""
				for name := range test.files {
					if filepath.Base(name) == "SKILL.md" && strings.Contains(string(test.files[name].Data), "name: "+entry.Skill.Name+"\n") {
						sourceFile = name
						break
					}
				}
				if entry.Source.Directory != filepath.ToSlash(filepath.Dir(sourceFile)) {
					t.Errorf("source directory = %q, want relative directory for %s", entry.Source.Directory, sourceFile)
				}
				wantRecords[entry.Skill.Name] = skills.SkillSource{Location: managerSource, Directory: filepath.ToSlash(filepath.Dir(sourceFile))}
				data, readErr := afero.ReadFile(base, filepath.Join(entry.Directory, "SKILL.md"))
				if readErr != nil || string(data) != string(test.files[sourceFile].Data) {
					t.Errorf("installed definition = %q, %v", data, readErr)
				}
			}
			if diff := cmp.Diff(test.want, names, cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("installed names (-want +got):\n%s", diff)
			}
			if len(test.names) > 0 {
				for _, name := range []string{"alpha", "beta"} {
					selected := false
					for _, want := range test.want {
						if want == name {
							selected = true
						}
					}
					if !selected {
						if _, statErr := base.Stat(filepath.Join(directory, name)); !errors.Is(statErr, fs.ErrNotExist) {
							t.Errorf("unselected %s directory exists: %v", name, statErr)
						}
					}
				}
			}
			if len(test.want) > 0 {
				if diff := cmp.Diff(wantRecords, readManagerRecords(t, base, directory)); diff != "" {
					t.Fatalf("persisted records (-want +got):\n%s", diff)
				}
				loaded, loadErr := skills.Load(afero.NewIOFS(afero.NewBasePathFs(base, directory)))
				if loadErr != nil {
					t.Fatalf("installed skills fail existing loader: %v", loadErr)
				}
				for _, name := range test.want {
					found := false
					for _, skill := range loaded {
						if skill.Name == name {
							found = true
						}
					}
					if !found {
						t.Errorf("installed %s is not loadable", name)
					}
				}
				listed, listErr := manager.ListInstalled(t.Context(), directory)
				if listErr != nil {
					t.Fatal(listErr)
				}
				for _, name := range test.want {
					found := false
					for _, entry := range listed {
						if entry.Skill.Name == name && entry.Source != nil {
							found = true
						}
					}
					if !found {
						t.Errorf("%s installation is not persisted as managed", name)
					}
				}
			}
			for name, file := range test.files {
				if strings.HasPrefix(name, "alpha/scripts/") {
					data, readErr := afero.ReadFile(base, filepath.Join(directory, filepath.FromSlash(name)))
					if readErr != nil || string(data) != string(file.Data) {
						t.Errorf("supporting file %s = %q, %v", name, data, readErr)
					}
				}
			}
			for name, file := range test.existing {
				if name == "skills.json" {
					continue
				}
				data, readErr := afero.ReadFile(base, filepath.Join(directory, filepath.FromSlash(name)))
				if readErr != nil || string(data) != string(file.Data) {
					t.Errorf("existing file %s changed", name)
				}
			}
		})
	}
}

func TestManagerListInstalled(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		files           fstest.MapFS
		missing, cancel bool
		link            string
		want            []string
		managed         []string
		errText         string
		wantErr         error
	}{
		{name: "empty root"},
		{name: "missing root", missing: true},
		{name: "unmanaged skill", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, want: []string{"alpha"}},
		{name: "managed and unmanaged", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "beta/SKILL.md": {Data: []byte(betaSkill)}, "skills.json": {Data: []byte(alphaRecord)}}, want: []string{"alpha", "beta"}, managed: []string{"alpha"}},
		{name: "stale records are not installed skills", files: fstest.MapFS{"skills.json": {Data: []byte(alphaRecord)}}},
		{name: "ignores files and unrelated directories", files: fstest.MapFS{"README.md": {Data: []byte("hi")}, "notes/file.txt": {Data: []byte("hi")}}},
		{name: "invalid installed definition", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte("invalid")}}, errText: "SKILL.md"},
		{name: "corrupt metadata", files: fstest.MapFS{"skills.json": {Data: []byte("broken")}}, errText: "skills.json"},
		{name: "unsupported record version", files: fstest.MapFS{"skills.json": {Data: []byte(`{"version":99,"skills":{}}`)}}, errText: "version"},
		{name: "destination root symlink", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, link: "/skills", errText: "symlink"},
		{name: "record symlink", files: fstest.MapFS{"skills.json": {Data: []byte(alphaRecord)}}, link: "/skills/skills.json", errText: "regular"},
		{name: "unsafe record key", files: fstest.MapFS{"skills.json": {Data: []byte(`{"version":1,"skills":{"../outside":{"location":"source","directory":"alpha"}}}`)}}, errText: "record"},
		{name: "cancelled", cancel: true, wantErr: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := filepath.FromSlash("/skills")
			destinations := newManagerFilesystem(t, directory, test.files)
			if test.missing {
				if err := destinations.RemoveAll(directory); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotManagerFS(t, destinations)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			filesystem := destinations
			if test.link != "" {
				filesystem = managerLinkFS{Fs: destinations, links: map[string]bool{test.link: true}}
			}
			got, err := skills.NewManager(nil, filesystem).ListInstalled(ctx, directory)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want cause %v", err, test.wantErr)
				}
			} else if test.errText != "" {
				if err == nil || !strings.Contains(err.Error(), test.errText) {
					t.Fatalf("error = %v, want %q", err, test.errText)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err == nil {
				var names, managed []string
				for _, entry := range got {
					names = append(names, entry.Skill.Name)
					if entry.Directory != filepath.Join(directory, entry.Skill.Name) {
						t.Errorf("directory: %q", entry.Directory)
					}
					if entry.Source != nil {
						managed = append(managed, entry.Skill.Name)
					}
				}
				if diff := cmp.Diff(test.want, names, cmpopts.EquateEmpty()); diff != "" {
					t.Error(diff)
				}
				if diff := cmp.Diff(test.managed, managed, cmpopts.EquateEmpty()); diff != "" {
					t.Error(diff)
				}
			}
			if diff := cmp.Diff(before, snapshotManagerFS(t, destinations)); diff != "" {
				t.Fatalf("listing mutated files:\n%s", diff)
			}
		})
	}
}

func TestManagerRemove(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name             string
		files            fstest.MapFS
		names, want      []string
		cancel, readOnly bool
		errText          string
		wantErr          error
	}{
		{name: "selected unmanaged skill", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "beta/SKILL.md": {Data: []byte(betaSkill)}}, names: []string{"alpha"}, want: []string{"alpha"}},
		{name: "managed skill and record", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "beta/SKILL.md": {Data: []byte(betaSkill)}, "skills.json": {Data: []byte(bothRecords)}}, names: []string{"alpha"}, want: []string{"alpha"}},
		{name: "duplicate selectors", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, names: []string{"alpha", "alpha"}, want: []string{"alpha"}},
		{name: "damaged unmanaged bundle", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte("invalid")}}, names: []string{"alpha"}, want: []string{"alpha"}},
		{name: "no names", errText: "select"},
		{name: "missing name preflight preserves other selections", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, names: []string{"alpha", "missing"}, wantErr: fs.ErrNotExist},
		{name: "path traversal", names: []string{"../outside"}, errText: "name"},
		{name: "absolute selector", names: []string{"/outside"}, errText: "name"},
		{name: "unrelated directory is preserved", files: fstest.MapFS{"notes/file.txt": {Data: []byte("keep")}}, names: []string{"notes"}, errText: "skill directory"},
		{name: "file is not a skill directory", files: fstest.MapFS{"alpha": {Data: []byte("keep")}}, names: []string{"alpha"}, errText: "directory"},
		{name: "corrupt records", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "skills.json": {Data: []byte("broken")}}, names: []string{"alpha"}, errText: "skills.json"},
		{name: "read only", files: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, names: []string{"alpha"}, readOnly: true, wantErr: fs.ErrPermission},
		{name: "cancelled", cancel: true, names: []string{"alpha"}, wantErr: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := filepath.FromSlash("/skills")
			base := newManagerFilesystem(t, directory, test.files)
			before := snapshotManagerFS(t, base)
			destinations := base
			if test.readOnly {
				destinations = afero.NewReadOnlyFs(base)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			got, err := skills.NewManager(nil, destinations).Remove(ctx, skills.RemoveRequest{Directory: directory, Skills: test.names})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want cause %v", err, test.wantErr)
				}
			} else if test.errText != "" {
				if err == nil || !strings.Contains(err.Error(), test.errText) {
					t.Fatalf("error = %v, want %q", err, test.errText)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err != nil {
				if diff := cmp.Diff(before, snapshotManagerFS(t, base)); diff != "" {
					t.Fatal(diff)
				}
				return
			}
			var paths []string
			for _, name := range test.want {
				paths = append(paths, filepath.Join(directory, name))
				if _, statErr := base.Stat(filepath.Join(directory, name)); !errors.Is(statErr, fs.ErrNotExist) {
					t.Errorf("removed bundle %s still exists: %v", name, statErr)
				}
			}
			if diff := cmp.Diff(paths, got); diff != "" {
				t.Error(diff)
			}
			if file, ok := test.files["beta/SKILL.md"]; ok {
				data, readErr := afero.ReadFile(base, filepath.Join(directory, "beta", "SKILL.md"))
				if readErr != nil || string(data) != string(file.Data) {
					t.Errorf("unselected bundle changed")
				}
			}
			if _, ok := test.files["skills.json"]; ok {
				records := readManagerRecords(t, base, directory)
				if _, ok := records["alpha"]; ok {
					t.Error("removed record remains")
				}
				if _, ok := records["beta"]; !ok {
					t.Error("unselected beta record removed")
				}
			}
		})
	}
}

func TestManagerUpdate(t *testing.T) {
	t.Parallel()
	fetchErr := errors.New("source unavailable")
	for _, test := range []struct {
		name             string
		existing, source fstest.MapFS
		fault            managerFSFault
		names            []string
		fetchErr         error
		cancel, readOnly bool
		want             []string
		wantErr          error
		errText          string
	}{
		{name: "empty destination"},
		{name: "unmanaged skill ignored", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}},
		{name: "updates managed bundle", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "alpha/obsolete.txt": {Data: []byte("old")}, "skills.json": {Data: []byte(alphaRecord)}}, source: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaUpdated)}, "alpha/new.txt": {Data: []byte("new")}}, want: []string{"alpha"}},
		{name: "selected managed skill only", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "beta/SKILL.md": {Data: []byte(betaSkill)}, "skills.json": {Data: []byte(bothRecords)}}, source: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaUpdated)}, "beta/SKILL.md": {Data: []byte("invalid")}}, names: []string{"alpha"}, want: []string{"alpha"}},
		{name: "recorded nested directory", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "skills.json": {Data: []byte(`{"version":1,"skills":{"alpha":{"location":"https://example.com/skills.git","directory":"a/b/c/d/e/alpha"}}}`)}}, source: fstest.MapFS{"a/b/c/d/e/alpha/SKILL.md": {Data: []byte(alphaUpdated)}}, want: []string{"alpha"}},
		{name: "recreates missing managed bundle", existing: fstest.MapFS{"skills.json": {Data: []byte(alphaRecord)}}, source: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaUpdated)}}, want: []string{"alpha"}},
		{name: "named unmanaged skill rejected", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}}, names: []string{"alpha"}, errText: "managed"},
		{name: "fetch failure preserves installed bundle", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "skills.json": {Data: []byte(alphaRecord)}}, fetchErr: fetchErr, wantErr: fetchErr},
		{name: "invalid incoming definition", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "skills.json": {Data: []byte(alphaRecord)}}, source: fstest.MapFS{"alpha/SKILL.md": {Data: []byte("invalid")}}, errText: "SKILL.md"},
		{name: "changed incoming name", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "skills.json": {Data: []byte(alphaRecord)}}, source: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(betaSkill)}}, errText: "directory"},
		{name: "corrupt records", existing: fstest.MapFS{"skills.json": {Data: []byte("broken")}}, errText: "skills.json"},
		{name: "recorded source ancestor becomes symlink", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "skills.json": {Data: []byte(`{"version":1,"skills":{"alpha":{"location":"https://example.com/skills.git","directory":"nested/alpha"}}}`)}}, source: fstest.MapFS{"nested": {Mode: fs.ModeSymlink}, "nested/alpha/SKILL.md": {Data: []byte(alphaUpdated)}}, errText: "symlink"},
		{name: "metadata commit failure restores old bundle", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "skills.json": {Data: []byte(alphaRecord)}}, source: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaUpdated)}}, fault: managerMetadataRenameFailure, wantErr: fs.ErrPermission},
		{name: "failed restore retains recovery data", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "skills.json": {Data: []byte(alphaRecord)}}, source: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaUpdated)}}, fault: managerRestoreFailure, wantErr: fs.ErrPermission},
		{name: "postcommit cleanup error exposes committed result", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "skills.json": {Data: []byte(alphaRecord)}}, source: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaUpdated)}}, fault: managerStageCleanupFailure, wantErr: fs.ErrPermission},
		{name: "unsafe recorded directory", existing: fstest.MapFS{"skills.json": {Data: []byte(`{"version":1,"skills":{"alpha":{"location":"source","directory":"../outside"}}}`)}}, errText: "record"},
		{name: "read only destination", existing: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaSkill)}, "skills.json": {Data: []byte(alphaRecord)}}, source: fstest.MapFS{"alpha/SKILL.md": {Data: []byte(alphaUpdated)}}, readOnly: true, wantErr: fs.ErrPermission},
		{name: "cancelled", cancel: true, wantErr: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := filepath.FromSlash("/skills")
			base := newManagerFilesystem(t, directory, test.existing)
			before := snapshotManagerFS(t, base)
			destinations := base
			if test.readOnly {
				destinations = afero.NewReadOnlyFs(base)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			if test.fault != managerNoFault {
				destinations = managerFaultFS{Fs: base, fault: test.fault}
			}
			got, err := skills.NewManager(managerSourceClient{files: test.source, err: test.fetchErr}, destinations).Update(ctx, skills.UpdateRequest{Directory: directory, Skills: test.names})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want cause %v", err, test.wantErr)
				}
			} else if test.errText != "" {
				if err == nil || !strings.Contains(err.Error(), test.errText) {
					t.Fatalf("error = %v, want %q", err, test.errText)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err != nil {
				if test.fault == managerRestoreFailure {
					if !strings.Contains(err.Error(), "retained") {
						t.Fatalf("missing recovery information: %v", err)
					}
					found := false
					for name, data := range snapshotManagerFS(t, base) {
						if strings.Contains(filepath.ToSlash(name), "/old/alpha/SKILL.md") && data == alphaSkill {
							found = true
						}
					}
					if !found {
						t.Fatal("original bundle destroyed after failed rollback")
					}
					return
				}
				if test.fault == managerStageCleanupFailure {
					if !strings.Contains(err.Error(), "committed") || len(got) != 1 || got[0].Skill.Name != "alpha" {
						t.Fatalf("committed outcome lost: %#v, %v", got, err)
					}
					data, readErr := afero.ReadFile(base, filepath.Join(directory, "alpha", "SKILL.md"))
					if readErr != nil || string(data) != alphaUpdated {
						t.Fatal("committed bundle missing")
					}
					return
				}
				if diff := cmp.Diff(before, snapshotManagerFS(t, base)); diff != "" {
					t.Fatalf("failed update changed files:\n%s", diff)
				}
				return
			}
			var names []string
			for _, entry := range got {
				names = append(names, entry.Skill.Name)
				if entry.Source == nil {
					t.Error("updated entry lost provenance")
				}
				data, readErr := afero.ReadFile(base, filepath.Join(entry.Directory, "SKILL.md"))
				if readErr != nil || string(data) != alphaUpdated {
					t.Errorf("updated definition = %q, %v", data, readErr)
				}
			}
			if diff := cmp.Diff(test.want, names, cmpopts.EquateEmpty()); diff != "" {
				t.Error(diff)
			}
			if len(got) > 0 {
				var previous struct {
					Skills map[string]skills.SkillSource `json:"skills"`
				}
				if err := json.Unmarshal(test.existing["skills.json"].Data, &previous); err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(previous.Skills, readManagerRecords(t, base, directory)); diff != "" {
					t.Fatalf("update changed provenance records:\n%s", diff)
				}
				for _, entry := range got {
					source := previous.Skills[entry.Skill.Name]
					if diff := cmp.Diff(&source, entry.Source); diff != "" {
						t.Errorf("updated provenance:\n%s", diff)
					}
				}
			}
			if len(got) == 0 {
				if diff := cmp.Diff(before, snapshotManagerFS(t, base)); diff != "" {
					t.Fatal(diff)
				}
			}
			if _, ok := test.existing["alpha/obsolete.txt"]; ok {
				if _, statErr := base.Stat(filepath.Join(directory, "alpha", "obsolete.txt")); !errors.Is(statErr, fs.ErrNotExist) {
					t.Error("obsolete payload survived update")
				}
				data, readErr := afero.ReadFile(base, filepath.Join(directory, "alpha", "new.txt"))
				if readErr != nil || string(data) != "new" {
					t.Error("new supporting payload missing")
				}
			}
			if file, ok := test.existing["beta/SKILL.md"]; ok {
				data, readErr := afero.ReadFile(base, filepath.Join(directory, "beta", "SKILL.md"))
				if readErr != nil || string(data) != string(file.Data) {
					t.Error("unselected beta changed")
				}
			}
		})
	}
}

// Source acquisition is the only mocked Manager collaborator.
type managerSourceClient struct {
	files   fs.FS
	err     error
	cleanup func() error
}

func (client managerSourceClient) Fetch(_ context.Context, source string) (skills.Checkout, error) {
	if source != managerSource {
		return skills.Checkout{}, errors.New("unexpected source locator: " + source)
	}
	if client.err != nil {
		return skills.Checkout{}, client.err
	}
	return skills.Checkout{FS: client.files, Cleanup: client.cleanup}, nil
}
func newManagerFilesystem(t *testing.T, root string, files fstest.MapFS) afero.Fs {
	t.Helper()
	filesystem := afero.NewMemMapFs()
	if err := filesystem.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, file := range files {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := filesystem.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := afero.WriteFile(filesystem, filename, file.Data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filesystem
}
func snapshotManagerFS(t *testing.T, filesystem afero.Fs) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := afero.Walk(filesystem, string(filepath.Separator), func(name string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			result[name] = "<directory>"
			return nil
		}
		data, err := afero.ReadFile(filesystem, name)
		if err != nil {
			return err
		}
		result[name] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func readManagerRecords(t *testing.T, filesystem afero.Fs, root string) map[string]skills.SkillSource {
	t.Helper()
	data, err := afero.ReadFile(filesystem, filepath.Join(root, "skills.json"))
	if err != nil {
		t.Fatal(err)
	}
	var records struct {
		Version int                           `json:"version"`
		Skills  map[string]skills.SkillSource `json:"skills"`
	}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	if records.Version != 1 {
		t.Fatalf("record version = %d", records.Version)
	}
	return records.Skills
}
