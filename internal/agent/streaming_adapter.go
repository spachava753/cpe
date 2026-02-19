package agent

import (
	"context"

	"github.com/spachava753/gai"
)

// streamingAdapterWithMetadataPropagate wraps a gai.StreamingAdapter and
// propagates specified keys from UsageMetadata to every block's ExtraFields
// after generation completes. This bridges a behavioral gap where the
// streaming path puts certain metadata (like ResponsesPrevRespId) only in
// the metadata block, while the non-streaming path puts it in every block's
// ExtraFields.
type streamingAdapterWithMetadataPropagate struct {
	gai.StreamingAdapter
	// propagateKeys lists the UsageMetadata keys to copy into block ExtraFields.
	propagateKeys []string
}

// Compile-time check: streamingAdapterWithMetadataPropagate must satisfy
// ToolCapableGenerator (Register is inherited from the embedded
// StreamingAdapter; Generate is overridden here).
var _ gai.ToolCapableGenerator = (*streamingAdapterWithMetadataPropagate)(nil)

func (s *streamingAdapterWithMetadataPropagate) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	resp, err := s.StreamingAdapter.Generate(ctx, dialog, options)
	if err != nil {
		return resp, err
	}

	// Collect values to propagate from UsageMetadata
	extraFields := make(map[string]interface{})
	for _, key := range s.propagateKeys {
		if val, ok := resp.UsageMetadata[key]; ok {
			extraFields[key] = val
		}
	}
	if len(extraFields) == 0 {
		return resp, nil
	}

	// Propagate to all blocks in all candidates
	for i := range resp.Candidates {
		for j := range resp.Candidates[i].Blocks {
			blk := &resp.Candidates[i].Blocks[j]
			if blk.ExtraFields == nil {
				blk.ExtraFields = make(map[string]interface{})
			}
			for k, v := range extraFields {
				// Don't overwrite if the block already has this key
				if _, exists := blk.ExtraFields[k]; !exists {
					blk.ExtraFields[k] = v
				}
			}
		}
	}

	return resp, nil
}
