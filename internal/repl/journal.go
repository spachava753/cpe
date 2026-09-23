package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"syscall"

	"github.com/spachava753/cpe/internal/session"
)

const errorEOF = "eof"

type hostCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}
type hostResult struct {
	CallID string          `json:"callId"`
	Value  json.RawMessage `json:"value"`
	Error  *recordedError  `json:"error,omitempty"`
}
type recordedError struct {
	Message string `json:"message"`
	Kind    string `json:"kind,omitempty"`
	Errno   int    `json:"errno,omitempty"`
}

func saveError(err error) *recordedError {
	if err == nil {
		return nil
	}
	e := &recordedError{Message: err.Error()}
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		e.Errno = int(errno)
	}
	for _, v := range []struct {
		k string
		e error
	}{{errorEOF, io.EOF}, {"not_exist", fs.ErrNotExist}, {"exist", fs.ErrExist}, {"permission", fs.ErrPermission}, {"closed", fs.ErrClosed}, {"invalid", fs.ErrInvalid}, {"canceled", context.Canceled}, {"deadline", context.DeadlineExceeded}} {
		if errors.Is(err, v.e) {
			e.Kind = v.k
			break
		}
	}
	return e
}
func (e *recordedError) Error() string { return e.Message }
func (e *recordedError) Unwrap() error {
	if e.Errno != 0 {
		return syscall.Errno(e.Errno)
	}
	switch e.Kind {
	case errorEOF:
		return io.EOF
	case "not_exist":
		return fs.ErrNotExist
	case "exist":
		return fs.ErrExist
	case "permission":
		return fs.ErrPermission
	case "closed":
		return fs.ErrClosed
	case "invalid":
		return fs.ErrInvalid
	case "canceled":
		return context.Canceled
	case "deadline":
		return context.DeadlineExceeded
	}
	return nil
}

type journal struct {
	store    *session.Store
	replay   bool
	records  []session.Entry
	position int
	fatal    error
	active   bool
}

// boundary is the only route to a live capability. The closure is never called
// during replay, including when records are missing or corrupt.
func boundary[T any](j *journal, name string, args any, live func() (T, error)) (T, error) {
	var zero T
	if j.fatal != nil {
		return zero, j.fatal
	}
	if !j.active {
		return zero, errors.New("host call outside evaluation")
	}
	data, err := json.Marshal(args)
	if err != nil {
		j.fatal = err
		return zero, err
	}
	if j.replay {
		if j.position+1 >= len(j.records) {
			j.fatal = fmt.Errorf("replay missing result for %s", name)
			return zero, j.fatal
		}
		callEntry, resultEntry := j.records[j.position], j.records[j.position+1]
		var call hostCall
		var result hostResult
		if err := json.Unmarshal(callEntry.Data, &call); err != nil {
			j.fatal = err
			return zero, err
		}
		if err := json.Unmarshal(resultEntry.Data, &result); err != nil {
			j.fatal = err
			return zero, err
		}
		if callEntry.Type != "host_call" || resultEntry.Type != "host_result" || result.CallID != callEntry.ID || call.Name != name || !bytes.Equal(call.Args, data) {
			j.fatal = fmt.Errorf("replay diverged at %s (recorded %s)", name, call.Name)
			return zero, j.fatal
		}
		j.position += 2
		var value T
		if err := json.Unmarshal(result.Value, &value); err != nil {
			j.fatal = err
			return zero, err
		}
		if result.Error != nil {
			if result.Error.Kind == errorEOF {
				return value, io.EOF
			}
			return value, result.Error
		}
		return value, nil
	}
	id, err := j.store.Append("host_call", hostCall{Name: name, Args: data})
	if err != nil {
		j.fatal = err
		return zero, err
	}
	value, callErr := live()
	encoded, err := json.Marshal(value)
	if err != nil {
		j.fatal = err
		return zero, err
	}
	if _, err := j.store.Append("host_result", hostResult{CallID: id, Value: encoded, Error: saveError(callErr)}); err != nil {
		j.fatal = err
		return zero, err
	}
	return value, callErr
}
func effect(j *journal, name string, args any, live func() error) error {
	_, err := boundary(j, name, args, func() (struct{}, error) { return struct{}{}, live() })
	return err
}
