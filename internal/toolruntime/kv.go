package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"

	"github.com/spachava753/dyson"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spachava753/starlarkx/starlarkstruct"
)

// KeyValueStore persists session-owned encoded data. Get returns an owned copy;
// Store borrows its bytes until return and publishes only after persistence succeeds.
// Missing keys return errors. Keys returns an owned list.
type KeyValueStore interface {
	Get(context.Context, string) ([]byte, error)
	Store(context.Context, string, []byte) error
	Keys(context.Context) ([]string, error)
}

func (h *toolHost) kvModule() *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "kv",
		Members: starlark.StringDict{
			"get": starlark.NewBuiltin(
				"kv.get",
				h.kvGet,
			),
			"store": starlark.NewBuiltin(
				"kv.store",
				h.kvStore,
			),
			"keys": starlark.NewBuiltin(
				"kv.keys",
				h.kvKeys,
			),
		},
	}
}

func (h *toolHost) kvGet(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"key",
		&key,
	); err != nil {
		return nil, err
	}
	data, err := h.config.KV.Get(
		dyson.EvaluationContext(thread),
		key,
	)
	if err != nil {
		return nil, err
	}
	var value kvData
	if err := json.Unmarshal(
		data,
		&value,
	); err != nil {
		return nil, err
	}
	return value.decode()
}

func (h *toolHost) kvStore(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	var value starlark.Value
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"key",
		&key,
		"value",
		&value,
	); err != nil {
		return nil, err
	}
	if err := checkKVData(
		value,
		make(map[starlark.Value]bool),
	); err != nil {
		return nil, err
	}
	data, err := json.Marshal(encodeKV(value))
	if err != nil {
		return nil, err
	}
	if err := h.config.KV.Store(
		dyson.EvaluationContext(thread),
		key,
		data,
	); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

func (h *toolHost) kvKeys(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
	); err != nil {
		return nil, err
	}
	keys, err := h.config.KV.Keys(dyson.EvaluationContext(thread))
	if err != nil {
		return nil, err
	}
	slices.Sort(keys)
	values := make(
		[]starlark.Value,
		len(keys),
	)
	for i, key := range keys {
		values[i] = starlark.String(key)
	}
	return starlark.NewList(values), nil
}

// Tags preserve Starlark types; byte payloads also preserve non-UTF-8 strings.
// Dictionaries use alternating key/value items to retain arbitrary hashable keys.
type kvData struct {
	Kind  string   `json:"kind"`
	Data  []byte   `json:"data,omitempty"`
	Items []kvData `json:"items,omitempty"`
}

func encodeKV(value starlark.Value) kvData {
	data := kvData{
		Kind: value.Type(),
	}
	switch value := value.(type) {
	case starlark.NoneType:
	case starlark.Bool, starlark.Int:
		data.Data = []byte(value.String())
	case starlark.Float:
		data.Data = []byte(strconv.FormatFloat(
			float64(value),
			'g',
			-1,
			64,
		))
	case starlark.String:
		data.Data = []byte(value)
	case starlark.Bytes:
		data.Data = []byte(value)
	case *starlark.List:
		for i := range value.Len() {
			data.Items = append(
				data.Items,
				encodeKV(value.Index(i)),
			)
		}
	case starlark.Tuple:
		for _, item := range value {
			data.Items = append(
				data.Items,
				encodeKV(item),
			)
		}
	case *starlark.Dict:
		for _, pair := range value.Items() {
			data.Items = append(
				data.Items,
				encodeKV(pair[0]),
				encodeKV(pair[1]),
			)
		}
	}
	return data
}

func (data kvData) decode() (starlark.Value, error) {
	switch data.Kind {
	case "NoneType":
		return starlark.None, nil
	case "bool":
		if string(data.Data) == "True" {
			return starlark.True, nil
		}
		if string(data.Data) == "False" {
			return starlark.False, nil
		}
	case "int":
		value, ok := new(big.Int).SetString(
			string(data.Data),
			10,
		)
		if ok {
			return starlark.MakeBigInt(value), nil
		}
	case "float":
		value, err := strconv.ParseFloat(
			string(data.Data),
			64,
		)
		return starlark.Float(value), err
	case "string":
		return starlark.String(data.Data), nil
	case "bytes":
		return starlark.Bytes(data.Data), nil
	case "list", "tuple", "dict":
		items := make(
			[]starlark.Value,
			len(data.Items),
		)
		for i, item := range data.Items {
			value, err := item.decode()
			if err != nil {
				return nil, err
			}
			items[i] = value
		}
		switch data.Kind {
		case "list":
			return starlark.NewList(items), nil
		case "tuple":
			return starlark.Tuple(items), nil
		case "dict":
			if len(items)%2 != 0 {
				return nil, errors.New("invalid stored dictionary")
			}
			result := starlark.NewDict(len(items) / 2)
			for i := 0; i < len(items); i += 2 {
				if err := result.SetKey(
					items[i],
					items[i+1],
				); err != nil {
					return nil, err
				}
			}
			return result, nil
		}
	}
	return nil, errors.New("invalid stored KV data")
}

// Only lists and dictionaries can introduce cycles; tuples can refer back to
// them but cannot form cycles on their own through the Starlark API.
func checkKVData(value starlark.Value, visiting map[starlark.Value]bool) error {
	switch value := value.(type) {
	case starlark.NoneType, starlark.Bool, starlark.Int, starlark.Float, starlark.String, starlark.Bytes:
		return nil
	case *starlark.List, *starlark.Dict:
		if visiting[value] {
			return errors.New("KV values must not contain cycles")
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
			if err := checkKVData(
				value.Index(i),
				visiting,
			); err != nil {
				return err
			}
		}
	case starlark.Tuple:
		for _, item := range value {
			if err := checkKVData(
				item,
				visiting,
			); err != nil {
				return err
			}
		}
	case *starlark.Dict:
		for _, pair := range value.Items() {
			key, item := pair[0], pair[1]
			if err := checkKVData(
				key,
				visiting,
			); err != nil {
				return err
			}
			if err := checkKVData(
				item,
				visiting,
			); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf(
			"KV values must be data; got %s",
			value.Type(),
		)
	}
	return nil
}
