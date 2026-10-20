package conversation

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/models"
)

func TestSessionStore(t *testing.T) {
	for _, scenario := range []string{
		"round trip",
		"cancelled save",
		"pagination",
		"invalid ID",
		"corrupt checkpoint",
		"delete",
	} {
		t.Run(
			scenario,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				store := &fileSessionStore{
					filesystem: filesystem,
					directory:  "/sessions",
				}
				record := Record{
					Compaction: &Compaction{
						Summary:       "Greeting received.",
						AfterMessages: 1,
					},
					Info: Info{
						ID:        "a",
						Cwd:       "/work",
						Title:     "first",
						UpdatedAt: time.Now().UTC(),
					},
					Options: models.Selection{
						Model:     "one",
						Reasoning: "high",
					},
					Dialog: gai.Dialog{
						{
							Role: gai.User,
							Blocks: []gai.Block{
								gai.TextBlock("hello"),
								gai.ImageBlock(
									[]byte("binary"),
									"image/png",
								),
							},
							ExtraFields: map[string]any{
								"signature": "keep me",
							},
						},
					},
				}
				if err := store.Save(
					t.Context(),
					record,
				); err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "round trip":
					loaded, err := store.Load(
						t.Context(),
						"a",
					)
					if err != nil {
						t.Fatal(err)
					}
					if loaded.Options != record.Options || loaded.Dialog[0].Blocks[0].Content.String() != "hello" || loaded.Dialog[0].Blocks[1].Content.String() != record.Dialog[0].Blocks[1].Content.String() || loaded.Dialog[0].ExtraFields["signature"] != "keep me" {
						t.Fatalf(
							"round trip: %#v",
							loaded,
						)
					}
					if loaded.Compaction == nil || *loaded.Compaction != *record.Compaction {
						t.Fatal("compaction was not persisted")
					}
					loaded.Compaction.Summary = "changed"
					loaded.Dialog[0].Blocks[0] = gai.TextBlock("changed")
					again, err := store.Load(
						t.Context(),
						"a",
					)
					if err != nil || again.Dialog[0].Blocks[0].Content.String() != "hello" || again.Compaction.Summary != "Greeting received." {
						t.Fatalf(
							"borrowed persisted state: %v",
							err,
						)
					}
					info, err := filesystem.Stat("/sessions/a.json")
					if err != nil || info.Mode().Perm() != 0o600 {
						t.Fatalf(
							"checkpoint permissions: %v %v",
							info,
							err,
						)
					}
				case "cancelled save":
					ctx, cancel := context.WithCancel(t.Context())
					cancel()
					record.Info.Title = "replacement"
					if err := store.Save(
						ctx,
						record,
					); !errors.Is(
						err,
						context.Canceled,
					) {
						t.Fatalf(
							"save: %v",
							err,
						)
					}
					loaded, err := store.Load(
						t.Context(),
						"a",
					)
					if err != nil || loaded.Info.Title != "first" {
						t.Fatalf(
							"cancelled save changed checkpoint: %v %v",
							loaded,
							err,
						)
					}
				case "pagination":
					record.Info.ID = "b"
					if err := store.Save(
						t.Context(),
						record,
					); err != nil {
						t.Fatal(err)
					}
					page, err := store.List(
						t.Context(),
						Query{
							Limit: 1,
							Cwd:   "/work",
						},
					)
					if err != nil || len(page.Sessions) != 1 || page.NextCursor == "" {
						t.Fatalf(
							"first page: %v %v",
							page,
							err,
						)
					}
					next, err := store.List(
						t.Context(),
						Query{
							Limit:  1,
							Cursor: page.NextCursor,
						},
					)
					if err != nil || len(next.Sessions) != 1 || next.NextCursor != "" || next.Sessions[0].ID == page.Sessions[0].ID {
						t.Fatalf(
							"second page: %v %v",
							next,
							err,
						)
					}
				case "invalid ID":
					if _, err := store.Load(
						t.Context(),
						"../outside",
					); err == nil {
						t.Fatal("accepted traversal ID")
					}
				case "corrupt checkpoint":
					if err := afero.WriteFile(
						filesystem,
						"/sessions/a.json",
						[]byte("{broken"),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
					if _, err := store.Load(
						t.Context(),
						"a",
					); err == nil {
						t.Fatal("accepted corrupt checkpoint")
					}
				case "delete":
					if err := store.Delete(
						t.Context(),
						"a",
					); err != nil {
						t.Fatal(err)
					}
					if _, err := store.Load(
						t.Context(),
						"a",
					); !errors.Is(
						err,
						fs.ErrNotExist,
					) {
						t.Fatalf(
							"load deleted: %v",
							err,
						)
					}
				}
				entries, err := afero.ReadDir(
					filesystem,
					"/sessions",
				)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.HasPrefix(
						entry.Name(),
						".checkpoint-",
					) {
						t.Fatal("temporary checkpoint leaked")
					}
				}
			},
		)
	}
}
