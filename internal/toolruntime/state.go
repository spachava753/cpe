package toolruntime

import (
	"errors"
	"fmt"

	"github.com/spachava753/starlarkx/starlark"
)

// sessionState admits only data and freezes it before publication. This also
// prevents aliases from inserting runtime objects into already-stored containers.
type sessionState struct {
	entries *starlark.Dict
}

func (s *sessionState) String() string             { return s.entries.String() }
func (*sessionState) Type() string                 { return "state" }
func (s *sessionState) Freeze()                    { s.entries.Freeze() }
func (s *sessionState) Truth() starlark.Bool       { return s.entries.Truth() }
func (*sessionState) Hash() (uint32, error)        { return 0, errors.New("state is not hashable") }
func (s *sessionState) Len() int                   { return s.entries.Len() }
func (s *sessionState) Iterate() starlark.Iterator { return s.entries.Iterate() }
func (s *sessionState) Items() []starlark.Tuple    { return s.entries.Items() }

func (s *sessionState) Get(key starlark.Value) (starlark.Value, bool, error) {
	if _, ok := key.(starlark.String); !ok {
		return nil, false, errors.New("state keys must be strings")
	}
	return s.entries.Get(key)
}

func (s *sessionState) SetKey(key, value starlark.Value) error {
	if _, ok := key.(starlark.String); !ok {
		return errors.New("state keys must be strings")
	}
	if err := checkStateData(
		value,
		make(map[starlark.Value]bool),
	); err != nil {
		return err
	}
	value.Freeze()
	return s.entries.SetKey(
		key,
		value,
	)
}

// Only lists and dictionaries can introduce cycles; tuples can refer back to
// them but cannot form cycles on their own through the Starlark API.
func checkStateData(value starlark.Value, visiting map[starlark.Value]bool) error {
	switch value := value.(type) {
	case starlark.NoneType, starlark.Bool, starlark.Int, starlark.Float, starlark.String, starlark.Bytes:
		return nil
	case *starlark.List, *starlark.Dict:
		if visiting[value] {
			return errors.New("state values must not contain cycles")
		}
		visiting[value] = true
		defer delete(
			visiting,
			value,
		)
	}
	switch value := value.(type) {
	case *starlark.List:
		for i := range value.Len() {
			if err := checkStateData(
				value.Index(i),
				visiting,
			); err != nil {
				return err
			}
		}
	case starlark.Tuple:
		for _, item := range value {
			if err := checkStateData(
				item,
				visiting,
			); err != nil {
				return err
			}
		}
	case *starlark.Dict:
		for _, pair := range value.Items() {
			key, item := pair[0], pair[1]
			if err := checkStateData(
				key,
				visiting,
			); err != nil {
				return err
			}
			if err := checkStateData(
				item,
				visiting,
			); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf(
			"state values must be data; got %s",
			value.Type(),
		)
	}
	return nil
}
