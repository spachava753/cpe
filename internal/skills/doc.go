// Package skills loads and manages Agent Skills bundles containing SKILL.md.
// Manager returns parsed results; Install, Update, List, and Remove also resolve
// command scope and write terminal or JSON summaries. Install and Remove maintain
// skill lock records, and Update refreshes tracked bundles from their sources.
// Specification warnings are retained in Skill.Violations.
package skills
