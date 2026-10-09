package auth

import (
	"context"
	"fmt"

	"github.com/spachava753/cpe/internal/commandio"
)

// LogoutOptions selects the provider and shared credential file.
type LogoutOptions struct {
	Provider Provider
	// Path is the native path to the shared auth.json file.
	Path string
}

// Logout removes the provider's entry from opts.Path and writes a confirmation.
// Other providers and remote sessions are preserved; output errors may follow a successful save.
func Logout(ctx context.Context, store *Store, opts LogoutOptions, streams commandio.IO) error {
	if err := store.Save(
		ctx,
		opts.Path,
		opts.Provider,
		Credentials{},
	); err != nil {
		return err
	}
	name, err := opts.Provider.name()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		streams.Output(),
		"Removed saved %s credentials.\n",
		name,
	)
	return err
}
