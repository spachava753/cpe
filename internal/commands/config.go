package commands

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spachava753/cpe/internal/config"
)

// ConfigAddOptions contains dependencies and user inputs for ConfigAdd.
// The CLI layer maps flags/args into this struct.
type ConfigAddOptions struct {
	ModelSpec  string    // provider/model-id format
	ConfigPath string    // path to config file (empty for default)
	Ref        string    // optional custom ref name
	Writer     io.Writer // output writer
}

// ConfigAdd resolves a provider/model spec through models.dev and persists the
// resulting model entry into the target config file.
func ConfigAdd(ctx context.Context, opts ConfigAddOptions) error {
	// Parse provider/model-id
	parts := strings.SplitN(opts.ModelSpec, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid model spec %q: expected format <provider>/<model-id>", opts.ModelSpec)
	}
	providerID, modelID := parts[0], parts[1]

	// Fetch models.dev registry
	registry, err := config.FetchModelsDevRegistry(ctx)
	if err != nil {
		return fmt.Errorf("fetching models.dev registry: %w", err)
	}

	// Look up provider and model
	provider, model, err := config.LookupRegistryModel(registry, providerID, modelID)
	if err != nil {
		return err
	}

	// Build model config from registry data
	modelCfg, err := config.BuildModelFromRegistry(provider, model, opts.Ref)
	if err != nil {
		return err
	}

	// Determine config path
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = config.FindDefaultConfigPath()
	}

	// Load or create config
	rawCfg, err := config.LoadOrCreateRawConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Add the model
	if err := rawCfg.AddModel(*modelCfg); err != nil {
		return err
	}

	// Write back to file
	if err := config.WriteRawConfig(configPath, rawCfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Fprintf(opts.Writer, "Added model %s/%s as %q to %s\n", providerID, modelID, modelCfg.Ref, configPath)
	return nil
}

// ConfigRemoveOptions contains dependencies and user inputs for ConfigRemove.
type ConfigRemoveOptions struct {
	Ref        string    // model ref to remove
	ConfigPath string    // path to config file (empty for default)
	Writer     io.Writer // output writer
}

// ConfigRemove removes one model reference from the target config file and
// writes the updated config back to disk.
func ConfigRemove(ctx context.Context, opts ConfigRemoveOptions) error {
	// Determine config path
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = config.FindDefaultConfigPath()
	}

	// Load config
	rawCfg, err := config.LoadRawConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Remove the model
	if err := rawCfg.RemoveModel(opts.Ref); err != nil {
		return err
	}

	// Write back to file
	if err := config.WriteRawConfig(configPath, rawCfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Fprintf(opts.Writer, "Removed model %q from %s\n", opts.Ref, configPath)
	return nil
}
