package theme

import (
	"bufio"
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/spachava753/cpe/internal/testutil/testgate"
)

func TestPortalRoundTripAndTimeout(t *testing.T) {
	testgate.RequireIntegration(t)
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux session D-Bus")
	}
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("requires dbus-daemon")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, daemon, "--session", "--nofork", "--print-address")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatalf("no bus address: %v", scanner.Err())
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", scanner.Text())
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.RequestName(portalDestination, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	mode := uint32(1)
	delayed := false
	methods := map[string]any{"ReadAll": func(_ []string) (map[string]map[string]dbus.Variant, *dbus.Error) {
		mu.Lock()
		value, slow := mode, delayed
		mu.Unlock()
		if slow {
			time.Sleep(250 * time.Millisecond)
		}
		return map[string]map[string]dbus.Variant{appearanceNamespace: {schemeSetting: dbus.MakeVariant(value), accentSetting: dbus.MakeVariant(struct{ R, G, B float64 }{.2, .4, .8})}}, nil
	}}
	if err := conn.ExportMethodTable(methods, portalPath, settingsInterface); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		mode uint32
		want string
	}{{1, darkTheme}, {2, lightTheme}, {0, ""}} {
		mu.Lock()
		mode = test.mode
		mu.Unlock()
		got := portalAppearance(ctx)
		if got.Mode != test.want || got.Accent != sampleSystemAccent {
			t.Fatalf("portal round trip: %+v", got)
		}
	}
	mu.Lock()
	delayed = true
	mu.Unlock()
	bounded, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	defer stop()
	start := time.Now()
	if got := portalAppearance(bounded); got != (Appearance{}) {
		t.Fatalf("timeout did not fall back: %+v", got)
	}
	if time.Since(start) > time.Second {
		t.Fatal("appearance request ignored deadline")
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	if got := portalAppearance(ctx); got != (Appearance{}) {
		t.Fatalf("missing bus did not fall back: %+v", got)
	}
}
