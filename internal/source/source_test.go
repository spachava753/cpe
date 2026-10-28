package source_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spachava753/cpe/internal/source"
)

type fetchClient func(context.Context, string) (source.Checkout, error)

func (client fetchClient) Fetch(ctx context.Context, locator string) (source.Checkout, error) {
	return client(
		ctx,
		locator,
	)
}

func TestAcquire(t *testing.T) {
	fetchErr := errors.New("fetch failed")
	cleanupErr := errors.New("cleanup failed")
	files := fstest.MapFS{
		"resource.txt": &fstest.MapFile{
			Data: []byte("resource"),
		},
	}
	for _, test := range []struct {
		fetchErr    error
		cleanupErr  error
		wantCause   error
		name        string
		locator     string
		errText     string
		wantFetch   int
		wantCleanup int
		noClient    bool
		canceled    bool
		noFS        bool
		noCleanup   bool
	}{
		{
			name:        "owned checkout",
			locator:     "fixture",
			wantFetch:   1,
			wantCleanup: 1,
		},
		{
			name:      "checkout without owned resources",
			locator:   "fixture",
			noCleanup: true,
			wantFetch: 1,
		},
		{
			name:        "caller receives cleanup failure",
			locator:     "fixture",
			cleanupErr:  cleanupErr,
			wantFetch:   1,
			wantCleanup: 1,
		},
		{
			name:     "missing client",
			locator:  "fixture",
			noClient: true,
			errText:  "unavailable",
		},
		{
			name:    "blank locator",
			locator: " \t ",
			errText: "source is required",
		},
		{
			name:      "canceled before fetching",
			locator:   "fixture",
			canceled:  true,
			wantCause: context.Canceled,
		},
		{
			name:      "fetch failure retains its cause",
			locator:   "fixture",
			fetchErr:  fetchErr,
			wantCause: fetchErr,
			wantFetch: 1,
		},
		{
			name:        "unusable checkout is released",
			locator:     "fixture",
			noFS:        true,
			errText:     "filesystem is unavailable",
			wantFetch:   1,
			wantCleanup: 1,
		},
		{
			name:      "unusable checkout without resources",
			locator:   "fixture",
			noFS:      true,
			noCleanup: true,
			errText:   "filesystem is unavailable",
			wantFetch: 1,
		},
		{
			name:        "invalid checkout retains cleanup failure",
			locator:     "fixture",
			noFS:        true,
			cleanupErr:  cleanupErr,
			errText:     "filesystem is unavailable",
			wantCause:   cleanupErr,
			wantFetch:   1,
			wantCleanup: 1,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.canceled {
					cancel()
				}
				var fetched, cleaned int
				var client source.Client = fetchClient(func(actualCtx context.Context, locator string) (source.Checkout, error) {
					fetched++
					if actualCtx != ctx || locator != test.locator {
						t.Fatal("fetch did not receive the caller's context and locator")
					}
					if test.fetchErr != nil {
						return source.Checkout{}, test.fetchErr
					}
					checkout := source.Checkout{
						FS: files,
					}
					if test.noFS {
						checkout.FS = nil
					}
					if !test.noCleanup {
						checkout.Cleanup = func() error {
							cleaned++
							return test.cleanupErr
						}
					}
					return checkout, nil
				})
				if test.noClient {
					client = nil
				}
				checkout, err := source.Acquire(
					ctx,
					client,
					test.locator,
				)
				if test.errText != "" || test.wantCause != nil {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) {
						t.Fatalf(
							"error = %v, want %q",
							err,
							test.errText,
						)
					}
					if test.wantCause != nil && !errors.Is(
						err,
						test.wantCause,
					) {
						t.Fatalf(
							"error = %v, want cause %v",
							err,
							test.wantCause,
						)
					}
					if checkout.FS != nil || checkout.Cleanup != nil {
						t.Fatal("failed acquisition returned a partial checkout")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if cleaned != 0 {
						t.Fatal("usable checkout was released before ownership transfer")
					}
					data, err := fs.ReadFile(
						checkout.FS,
						"resource.txt",
					)
					if err != nil || string(data) != "resource" {
						t.Fatalf(
							"checkout resource = %q, %v",
							data,
							err,
						)
					}
					if err := checkout.Close(); !errors.Is(
						err,
						test.cleanupErr,
					) {
						t.Fatalf(
							"close error = %v, want %v",
							err,
							test.cleanupErr,
						)
					}
				}
				if fetched != test.wantFetch || cleaned != test.wantCleanup {
					t.Fatalf(
						"fetches/cleanups = %d/%d, want %d/%d",
						fetched,
						cleaned,
						test.wantFetch,
						test.wantCleanup,
					)
				}
			},
		)
	}
}
