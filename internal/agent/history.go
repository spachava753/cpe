package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

// messageOrigin is separate from provider replay fields and never sent to APIs.
// Version 1 binds generated assistant data to a model/service and the exact
// projected request prefix (instructions, tools, and messages, excluding options).
type messageOrigin struct {
	Version  int           `json:"version"`
	Identity modelIdentity `json:"identity"`
	Prefix   string        `json:"prefix"`
}
type modelIdentity struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Service  string `json:"service"`
}

func identityOf(model config.Model) modelIdentity {
	// Store no credentials or potentially credential-bearing endpoint URL.
	data, _ := json.Marshal([]string{model.BaseURL, model.Credential, model.APIKeyEnv})
	service := sha256.Sum256(data)
	return modelIdentity{Provider: model.Provider, Model: model.ID, Service: hex.EncodeToString(service[:])}
}

type prefixState struct {
	digest hash.Hash
	err    error
}

func newPrefix(req gai.GenerationRequest) *prefixState {
	p := &prefixState{digest: sha256.New()}
	p.add(encodeMessage(req.Instructions))
	p.add(req.Tools)
	return p
}
func (p *prefixState) add(value any) {
	if p.err == nil {
		p.err = json.NewEncoder(p.digest).Encode(value)
	}
}
func (p *prefixState) sum() string {
	if p.err != nil {
		return ""
	}
	return hex.EncodeToString(p.digest.Sum(nil))
}
func (a *Agent) originFor(req gai.GenerationRequest) (*messageOrigin, error) {
	prefix := newPrefix(req)
	for _, m := range req.Dialog {
		prefix.add(encodeMessage(m))
	}
	if prefix.err != nil {
		return nil, prefix.err
	}
	return &messageOrigin{Version: 1, Identity: identityOf(a.opts.Model), Prefix: prefix.sum()}, nil
}

// projectRequest retains the canonical history and constructs a provider-facing
// view. Returning to a model restores its own replay data. Anthropic additionally
// requires the generating prefix, including the retained thinking chain, to match.
func (a *Agent) projectRequest(req gai.GenerationRequest) gai.GenerationRequest {
	identity := identityOf(a.opts.Model)
	prefix := newPrefix(req)
	dialog := make(gai.Dialog, 0, len(req.Dialog))
	for index, m := range req.Dialog {
		if m.Role == gai.Assistant {
			origin, known := a.origins[index]
			compatible := known && origin.Identity == identity
			if compatible && identity.Provider == "anthropic" {
				compatible = origin.Prefix == prefix.sum()
			}
			if !compatible {
				m.ExtraFields = nil
				blocks := make([]gai.Block, 0, len(m.Blocks))
				for _, block := range m.Blocks {
					if block.BlockType == gai.Thinking {
						continue
					}
					// Filename is portable media content metadata, not provider replay state.
					extra := block.ExtraFields[gai.BlockFieldFilenameKey]
					block.ExtraFields = nil
					if extra != nil {
						block.ExtraFields = map[string]any{gai.BlockFieldFilenameKey: extra}
					}
					blocks = append(blocks, block)
				}
				m.Blocks = blocks
			}
			if len(m.Blocks) == 0 {
				continue
			}
		}
		dialog = append(dialog, m)
		prefix.add(encodeMessage(m))
	}
	req.Dialog = dialog
	return req
}
