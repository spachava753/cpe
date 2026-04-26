package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/spachava753/cpe/internal/auth"
)

const accountUsageRefreshInterval = time.Second

func runOpenAIAccountUsage(ctx context.Context, opts AccountUsageOptions) error {
	if opts.Raw {
		return printOpenAIAccountUsageRaw(ctx, opts.Output, opts.BaseURL)
	}
	if opts.Watch {
		return watchOpenAIAccountUsage(ctx, opts.Output, opts.BaseURL)
	}
	return printOpenAIAccountUsageSnapshot(ctx, opts.Output, opts.BaseURL)
}

func printOpenAIAccountUsageRaw(ctx context.Context, output io.Writer, baseURL string) error {
	usage, err := fetchOpenAIAccountUsage(ctx, baseURL)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(output)
	enc.SetIndent("", "  ")
	if err := enc.Encode(usage); err != nil {
		return fmt.Errorf("writing usage output: %w", err)
	}
	return nil
}

func printOpenAIAccountUsageSnapshot(ctx context.Context, output io.Writer, baseURL string) error {
	usage, err := fetchOpenAIAccountUsage(ctx, baseURL)
	if err != nil {
		return err
	}

	now := time.Now()
	width := detectAccountUsageWidth(output)
	view := renderOpenAIUsageView(usage, openAIUsageViewOptions{
		Now:         now,
		LastUpdated: now,
		Width:       width,
	})
	_, err = fmt.Fprintln(output, view)
	return err
}

func watchOpenAIAccountUsage(ctx context.Context, output io.Writer, baseURL string) error {
	model := newOpenAIUsageWatchModel(ctx, baseURL)
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithOutput(output))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("running usage watch UI: %w", err)
	}
	return nil
}

func fetchOpenAIAccountUsage(ctx context.Context, baseURL string) (*auth.OpenAIUsageResponse, error) {
	store, err := auth.NewStore()
	if err != nil {
		return nil, fmt.Errorf("initializing auth store: %w", err)
	}

	cred, err := ensureFreshOpenAIAccountCredential(ctx, store)
	if err != nil {
		return nil, err
	}

	usage, err := auth.FetchOpenAIUsage(ctx, http.DefaultClient, baseURL, cred.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("retrieving openai account usage: %w", err)
	}
	return usage, nil
}

func ensureFreshOpenAIAccountCredential(ctx context.Context, store *auth.Store) (*auth.Credential, error) {
	cred, err := store.GetCredential(ProviderOpenAI)
	if err != nil {
		return nil, fmt.Errorf("getting credential: %w", err)
	}

	if cred.ExpiresAt == 0 || time.Now().Unix() < cred.ExpiresAt-60 {
		return cred, nil
	}
	if cred.RefreshToken == "" {
		return nil, fmt.Errorf("openai account token is expired and no refresh token is available; please run 'cpe account login openai' to re-authenticate")
	}

	tokenResp, err := auth.RefreshAccessTokenOpenAI(ctx, cred.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refreshing openai token: %w", err)
	}

	cred = auth.TokenToCredentialPreserveRefresh(ProviderOpenAI, tokenResp, cred.RefreshToken)
	if err := store.SaveCredential(cred); err != nil {
		return nil, fmt.Errorf("saving refreshed openai credential: %w", err)
	}
	return cred, nil
}

func detectAccountUsageWidth(output io.Writer) int {
	const fallbackWidth = 80
	file, ok := output.(*os.File)
	if !ok {
		return fallbackWidth
	}
	fd := int(file.Fd())
	if !term.IsTerminal(fd) {
		return fallbackWidth
	}
	width, _, err := term.GetSize(fd)
	if err != nil || width <= 0 {
		return fallbackWidth
	}
	return width
}
