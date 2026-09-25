// Package skills discovers Agent Skills and resolves explicit user invocations.
// Discover reads only YAML frontmatter from immediate child directories of each
// supplied root. Missing roots are empty; symlinked skill directories work.
// Later roots override earlier ones by directory name, even when the overriding
// SKILL.md is invalid. Invalid skills produce diagnostics and are not exposed.
// Names and descriptions follow agentskills.io/specification; unknown metadata
// is ignored. Frontmatter is bounded to 64 KiB. No scripts or bodies are loaded.
//
// A Catalog is an immutable startup snapshot. By default skills appear in both
// user commands and model instructions. The compatible top-level YAML extensions
// disable-model-invocation: true and user-invocable: false restrict those surfaces
// independently; both restrictions hide a skill from both. Flags must be YAML
// booleans. Invocation policy controls discovery, not filesystem access.
//
// Expand resolves /skill:NAME [arguments] into a user request to read the absolute
// SKILL.md path through the existing REPL. Arguments remain ordinary user text;
// CPE does not interpolate templates or execute inline commands. Instructions
// lists only model-invocable metadata, never skill bodies. Relative resources
// are resolved by the model against the skill directory. File reads and script
// calls use the REPL's existing durability; skills add no tools or journal types.
package skills
