package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/bradleyjkemp/cupaloy/v2"

	"github.com/spachava753/cpe/internal/storage"
)

func TestPrintMessageForest_SimpleTree(t *testing.T) {
	now := time.Date(2025, 04, 15, 14, 0, 0, 0, time.UTC)
	forest := []storage.MessageIdNode{
		{
			ID:        "A",
			CreatedAt: now,
			Children: []storage.MessageIdNode{
				{
					ID:        "B",
					CreatedAt: now.Add(1 * time.Minute),
					Children: []storage.MessageIdNode{
						{
							ID:        "C",
							CreatedAt: now.Add(2 * time.Minute),
						},
					},
				},
				{
					ID:        "D",
					CreatedAt: now.Add(3 * time.Minute),
					Children:  nil,
				},
			},
		},
		{
			ID:        "E",
			CreatedAt: now.Add(4 * time.Minute),
			Children:  nil,
		},
	}

	var buf bytes.Buffer
	PrintMessageForest(&buf, forest)

	cupaloy.SnapshotT(t, buf.String())
}
