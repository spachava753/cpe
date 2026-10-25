package models

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
)

type rejectModelRename struct {
	afero.Fs
}

func (rejectModelRename) Rename(string, string) error {
	return errors.New("fixture rename failure")
}

func TestLastUsed(t *testing.T) {
	for _, test := range []struct {
		name        string
		initial     string
		save        ID
		want        ID
		saveError   string
		loadError   string
		cancelled   bool
		rejectWrite bool
	}{
		{
			name: "no preference yet",
		},
		{
			name: "saved preference survives a new store instance",
			save: "first",
			want: "first",
		},
		{
			name:    "latest model replaces previous preference",
			initial: `{"model":"first"}`,
			save:    "second",
			want:    "second",
		},
		{
			name:      "corrupt preference is reported",
			initial:   `{broken`,
			loadError: "decode last-used model",
		},
		{
			name:      "empty model is reported",
			initial:   `{"model":""}`,
			loadError: "last-used model ID is empty",
		},
		{
			name:      "invalid write leaves previous preference intact",
			initial:   `{"model":"first"}`,
			save:      " ",
			saveError: "last-used model ID is empty",
			want:      "first",
		},
		{
			name:        "failed publication leaves previous preference intact",
			initial:     `{"model":"first"}`,
			save:        "second",
			rejectWrite: true,
			saveError:   "fixture rename failure",
			want:        "first",
		},
		{
			name:      "cancelled write leaves previous preference intact",
			initial:   `{"model":"first"}`,
			save:      "second",
			cancelled: true,
			saveError: "context canceled",
			want:      "first",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				const filename = "/config/last-model.json"
				if test.initial != "" {
					if err := filesystem.MkdirAll(
						"/config",
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := afero.WriteFile(
						filesystem,
						filename,
						[]byte(test.initial),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				store := &LastUsed{
					filesystem: filesystem,
					filename:   filename,
				}
				if test.rejectWrite {
					store.filesystem = rejectModelRename{
						Fs: filesystem,
					}
				}
				if test.save != "" {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					if test.cancelled {
						cancel()
					}
					err := store.Save(
						ctx,
						test.save,
					)
					if test.saveError == "" && err != nil || test.saveError != "" && (err == nil || !strings.Contains(
						err.Error(),
						test.saveError,
					)) {
						t.Fatalf(
							"save: %v; want %q",
							err,
							test.saveError,
						)
					}
					files, err := afero.ReadDir(
						filesystem,
						"/config",
					)
					if err != nil || len(files) != 1 || files[0].Name() != "last-model.json" {
						t.Fatalf(
							"preference staging files were left behind: %v, %v",
							files,
							err,
						)
					}
				}
				reopened := &LastUsed{
					filesystem: filesystem,
					filename:   filename,
				}
				got, err := reopened.Load(t.Context())
				if got != test.want || test.loadError == "" && err != nil || test.loadError != "" && (err == nil || !strings.Contains(
					err.Error(),
					test.loadError,
				)) {
					t.Fatalf(
						"load: %q, %v; want %q, %q",
						got,
						err,
						test.want,
						test.loadError,
					)
				}
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				if _, err := reopened.Load(ctx); !errors.Is(
					err,
					context.Canceled,
				) {
					t.Fatal("cancelled read did not stop")
				}
			},
		)
	}
}

func TestLastUsedConcurrentAccess(t *testing.T) {
	filename := filepath.Join(
		t.TempDir(),
		"last-model.json",
	)
	first, err := NewLastUsed(filename)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewLastUsed(filename)
	if err != nil {
		t.Fatal(err)
	}
	var work sync.WaitGroup
	for _, store := range []*LastUsed{
		first,
		second,
	} {
		for _, model := range []ID{
			"one",
			"two",
		} {
			work.Go(func() {
				for range 10 {
					if err := store.Save(
						t.Context(),
						model,
					); err != nil {
						t.Error(err)
						return
					}
					got, err := store.Load(t.Context())
					if err != nil || got != "one" && got != "two" {
						t.Errorf(
							"concurrent read: %q, %v",
							got,
							err,
						)
						return
					}
				}
			})
		}
	}
	work.Wait()
}
