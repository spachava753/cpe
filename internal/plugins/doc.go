// Package plugins discovers, validates, installs, lists, and removes native
// Agent Plugins 1.0.0 packages. Load reads immediate child directories of an
// io/fs filesystem; Manager discovers packages in acquired source trees. It
// supports skills and configuration for stdio, Streamable HTTP, and SSE MCP
// transports. Client-specific plugin formats and marketplace catalogs are not
// installation sources.
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
//
// Manager keeps acquisition and destination filesystems separate, copies entire
// selected bundles except Git metadata, and materializes contained relative
// symlinks into independent files and directories. Installation and removal
// stage all selected changes before publishing them; failed publication rolls
// back prior changes. Cleanup failures after publication return committed
// results alongside opaque errors. Installation never activates components.
//
// Install, List, and Remove are command-facing operations, separate from Manager.
// Their options and IO mirror the skills package. Default roots are
// ~/.agents/plugins and ./.agents/plugins; custom directories are exact roots.
// Output is a terminal table or a JSON summary array, with diagnostics on stderr.
package plugins
