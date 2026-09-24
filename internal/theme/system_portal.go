package theme

import (
	"context"

	"github.com/godbus/dbus/v5"
)

const schemeSetting = "color-scheme"
const accentSetting = "accent-color"

const appearanceNamespace = "org.freedesktop.appearance"
const portalDestination = "org.freedesktop.portal.Desktop"
const portalPath = "/org/freedesktop/portal/desktop"
const settingsInterface = "org.freedesktop.portal.Settings"

func portalAppearance(ctx context.Context) Appearance {
	// Do not launch a session bus in a headless process. A private connection
	// avoids leaving a shared connection or signal listener behind on reload.
	conn, err := dbus.SessionBusPrivateNoAutoStartup(dbus.WithContext(ctx))
	if err != nil {
		return Appearance{}
	}
	defer conn.Close()
	if err := conn.Auth(nil); err != nil {
		return Appearance{}
	}
	if err := conn.Hello(); err != nil {
		return Appearance{}
	}
	return readPortalAppearance(ctx, conn.Object(portalDestination, portalPath))
}

func readPortalAppearance(ctx context.Context, object dbus.BusObject) Appearance {
	// ReadAll is supported by both versions of the Settings interface and gives
	// mode and accent in one snapshot, including when accent-color is absent.
	call := object.CallWithContext(ctx, settingsInterface+".ReadAll", 0, []string{appearanceNamespace})
	if call.Err != nil || len(call.Body) != 1 {
		return Appearance{}
	}
	// Keep the decoder's variant signatures. Store on nested maps can rebuild a
	// struct variant as an array variant, losing the accent's (ddd) signature.
	namespaces, ok := call.Body[0].(map[string]map[string]dbus.Variant)
	if !ok {
		return Appearance{}
	}
	values := namespaces[appearanceNamespace]
	var a Appearance
	switch values[schemeSetting].Value() {
	case uint32(1):
		a.Mode = darkTheme
	case uint32(2):
		a.Mode = lightTheme
	}
	if accent, ok := values[accentSetting]; ok && accent.Signature().String() == "(ddd)" {
		var rgb struct{ R, G, B float64 }
		if dbus.Store([]any{accent.Value()}, &rgb) == nil {
			a.Accent = rgbColor([]float64{rgb.R, rgb.G, rgb.B})
		}
	}
	return a
}
