package storage

import "errors"

// ErrSessionNotFound indicates that persisted ACP session metadata was missing.
// Use errors.Is to test for it through contextual storage errors.
var ErrSessionNotFound = errors.New("ACP session not found")

// ErrSessionConflict indicates that another process advanced an ACP session
// from the expected last message before the requested update could be applied.
// Use errors.Is to test for it through contextual storage errors.
var ErrSessionConflict = errors.New("ACP session advancement conflict")

// ErrMessageNotFound indicates that persisted message data was missing.
// Use errors.Is to test for it through contextual storage errors.
var ErrMessageNotFound = errors.New("message not found")
