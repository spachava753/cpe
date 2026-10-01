package skills

import "io/fs"

// Skill represents a single skill, as defined by https://agentskills.io/specification
type Skill struct {
	// Name is the name of the skill. Max 64 characters
	Name string
	// Description is the skill description for what the skill does and when to use it. Max 1024 characters
	Description string
	// License name or reference to a bundled license file
	License string
	// Compatibility indicates environment requirements (intended product, system packages, network access, etc.). Max 500 characters
	Compatibility string
	// Metadata is arbitrary key-value mapping for additional metadata
	Metadata map[string]string
	// AllowedTools is an experimental field that is a space-separated string of tools that are pre-approved to run
	AllowedTools []string
	// InvocationDisabled is set to true if "disable-model-invocation" is exists and set to true
	InvocationDisabled bool
	// Path is the file path that the skill is stored in
	Path string
}

// Load searches for skills in the given path, and validates the skills encountered before returning them. It stops on the first invalid skill encountered and returns an error.
func Load(path fs.ReadDirFS) ([]Skill, error) {
	panic("unimplemented")
}
