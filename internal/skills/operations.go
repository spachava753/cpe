package skills

import "io"

// IO supplies the command's streams without coupling skills to Cobra. The
// actions own prompting, progress, and text/JSON results. Nil members request
// the corresponding process standard streams.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// ScopeOptions contains raw destination inputs. Actions own path resolution.
// A non-nil Directory selects only that exact skill directory; it may be combined
// with Project, but not Global. Otherwise Project or Global selects its default
// directory. When both are false, List uses both defaults, Remove searches both,
// and Install requires an explicit scope.
type ScopeOptions struct {
	Directory *string
	Project   bool
	Global    bool
}
