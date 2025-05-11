package version

import "runtime/debug"

// Get returns the version of the application from build info
func Get() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(unknown version)"
}
