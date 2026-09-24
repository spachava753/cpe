package theme

import (
	"context"
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
)

type portalFixture struct {
	dbus.BusObject
	call func(context.Context, string, dbus.Flags, ...any) *dbus.Call
}

func (f portalFixture) CallWithContext(ctx context.Context, method string, flags dbus.Flags, args ...any) *dbus.Call {
	return f.call(ctx, method, flags, args...)
}

func TestPortalAppearanceProtocol(t *testing.T) {
	for _, test := range []struct {
		name    string
		values  map[string]dbus.Variant
		failure error
		want    Appearance
	}{
		{name: "dark without accent", values: map[string]dbus.Variant{schemeSetting: dbus.MakeVariant(uint32(1))}, want: Appearance{Mode: darkTheme}},
		{name: "light with accent", values: map[string]dbus.Variant{schemeSetting: dbus.MakeVariant(uint32(2)), accentSetting: dbus.MakeVariantWithSignature([]any{.2, .4, .8}, dbus.ParseSignatureMust("(ddd)"))}, want: Appearance{Mode: lightTheme, Accent: sampleSystemAccent}},
		{name: "no preference", values: map[string]dbus.Variant{schemeSetting: dbus.MakeVariant(uint32(0))}},
		{name: "future mode", values: map[string]dbus.Variant{schemeSetting: dbus.MakeVariant(uint32(99))}},
		{name: "wrong mode type", values: map[string]dbus.Variant{schemeSetting: dbus.MakeVariant("dark")}},
		{name: "wrong accent type", values: map[string]dbus.Variant{schemeSetting: dbus.MakeVariant(uint32(1)), accentSetting: dbus.MakeVariant("blue")}, want: Appearance{Mode: darkTheme}},
		{name: "out of range accent", values: map[string]dbus.Variant{schemeSetting: dbus.MakeVariant(uint32(2)), accentSetting: dbus.MakeVariantWithSignature([]any{-1., 0., 0.}, dbus.ParseSignatureMust("(ddd)"))}, want: Appearance{Mode: lightTheme}},
		{name: "missing namespace"},
		{name: "unavailable service", failure: errors.New("unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			object := portalFixture{call: func(ctx context.Context, method string, flags dbus.Flags, args ...any) *dbus.Call {
				if ctx != t.Context() || method != settingsInterface+".ReadAll" || flags != 0 || len(args) != 1 {
					t.Fatalf("unexpected portal call: %s %v", method, args)
				}
				namespaces, ok := args[0].([]string)
				if !ok || len(namespaces) != 1 || namespaces[0] != appearanceNamespace {
					t.Fatalf("unexpected namespace: %v", args)
				}
				return &dbus.Call{Body: []any{map[string]map[string]dbus.Variant{appearanceNamespace: test.values}}, Err: test.failure}
			}}
			if got := readPortalAppearance(t.Context(), object); got != test.want {
				t.Fatalf("got %+v want %+v", got, test.want)
			}
		})
	}
}
