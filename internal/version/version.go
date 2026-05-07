package version

import "runtime/debug"

// value is populated by release builds with -ldflags -X.
var value string

// Get returns the version of the application from build info.
func Get() string {
	if value != "" {
		return value
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(unknown version)"
}
