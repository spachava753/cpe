package client

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spachava753/acp-sdk/acp"
)

type handler struct {
	out io.Writer
}

func (h *handler) RequestPermission(ctx context.Context, request *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	return &acp.RequestPermissionResponse{Outcome: acp.CancelledRequestPermissionOutcome()}, nil
}

func (h *handler) Update(ctx context.Context, notification *acp.SessionNotification) error {
	u := notification.Update
	switch u.SessionUpdate {
	case acp.SessionUpdateTypeAgentMessageChunk, acp.SessionUpdateTypeUserMessageChunk:
		fmt.Fprint(h.out, contentText(u.Content))
	case acp.SessionUpdateTypeAgentThoughtChunk:
		fmt.Fprintf(h.out, "\n[thought] %s\n", contentText(u.Content))
	case acp.SessionUpdateTypeToolCall:
		fmt.Fprintf(h.out, "\n[tool: %s]\n", value(u.Title))
	case acp.SessionUpdateTypeToolCallUpdate:
		parts := []string{string(u.ToolCallID)}
		if u.Title != nil && *u.Title != "" {
			parts = append(parts, *u.Title)
		}
		if u.Status != nil {
			parts = append(parts, string(*u.Status))
		}
		fmt.Fprintf(h.out, "\n[tool update: %s]\n", strings.Join(parts, " | "))
		if u.RawOutput != nil {
			fmt.Fprintf(h.out, "%s\n", marshal(u.RawOutput))
		}
	case acp.SessionUpdateTypePlan:
		for _, entry := range u.Entries {
			fmt.Fprintf(h.out, "\n[plan: %s] %s\n", entry.Status, entry.Content)
		}
	case acp.SessionUpdateTypePlanUpdate:
		if u.Plan.Content != "" {
			fmt.Fprintf(h.out, "\n[plan] %s\n", u.Plan.Content)
		}
		for _, entry := range u.Plan.Entries {
			fmt.Fprintf(h.out, "\n[plan: %s] %s\n", entry.Status, entry.Content)
		}
	case acp.SessionUpdateTypeUsageUpdate:
		fmt.Fprintf(h.out, "\n[usage: %d/%d]\n", u.Used, u.Size)
	}
	return nil
}
