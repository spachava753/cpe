package skills

import (
	"errors"
	"io"
)

// ErrNotImplemented reports that an action is still a stub. Stub calls perform
// no inspection, prompting, output, or mutation.
var ErrNotImplemented = errors.New("skills action is not implemented")

// IO supplies the command's streams without coupling skills to Cobra. The
// actions own prompting, progress, and text/JSON results. Nil members request
// the corresponding process standard streams when the actions are implemented.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// ScopeOptions contains raw destination inputs. Actions own path resolution.
// A non-nil Directory selects only that exact skill directory, with skills.json
// inside it; it may be combined with Project, but not Global. Otherwise Project
// or Global selects its default directory. When both are false, List and Update
// use both defaults, Remove searches both, and Install requires an explicit scope.
type ScopeOptions struct {
	Directory *string
	Project   bool
	Global    bool
}
