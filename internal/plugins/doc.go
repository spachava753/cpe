// Package plugins discovers and validates Agent Plugins 1.0.0 packages from
// immediate child directories of an io/fs filesystem. It supports skills and
// configuration for stdio, Streamable HTTP, and legacy SSE MCP transports.
//
// Invalid manifests are fatal to Load. Invalid components are isolated and
// returned as diagnostics on the otherwise valid plugin. Unknown manifest
// fields and unimplemented extension data have the specification's non-fatal
// behavior. Schemas are selected locally; loading performs no network requests.
//
// Loading is not activation. A launcher must supply the absolute PLUGIN_ROOT
// and persistent PLUGIN_DATA directories, expand the two placeholders once in
// args, env values, and cwd, and enforce filesystem containment after expansion.
// It must also apply the specification's environment, header precedence,
// authentication, and redirect rules. Load preserves the configuration strings
// and does not launch commands or resolve bare executable names through PATH.
//
// Package reads accept provably contained relative symlinks through ReadLinkFS.
// Absolute symlinks and plugin-root symlinks are rejected because an opaque FS
// does not expose the absolute, filesystem-resolved package root. Filesystem
// changes between validation and activation require fresh containment checks.
package plugins
