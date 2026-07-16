/*
Package skills discovers CPE skill directories and parses their metadata.

It parses SKILL.md frontmatter, preserves arbitrary metadata for prompt
templates, filters model-visible skills, and records stable display paths that
callers can present in their own protocol or UI layer. Discovery warnings use
the caller's context so structured request or session attributes remain attached.
*/
package skills
