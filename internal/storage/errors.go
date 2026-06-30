package storage

import "errors"

// ErrSessionNotFound indicates that persisted ACP session metadata was missing.
// Use errors.Is to test for it through contextual storage errors.
var ErrSessionNotFound = errors.New("ACP session not found")

// ErrMessageNotFound indicates that persisted message data was missing.
// Use errors.Is to test for it through contextual storage errors.
var ErrMessageNotFound = errors.New("message not found")
