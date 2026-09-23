// Package cli owns the small terminal command surface. With no flags CPE opens
// a fresh durable session. --resume continues one, --continue selects the newest
// session in the current directory, and --sessions lists matching session files.
// --init creates starter configuration, --model selects a JSON profile, and
// --prompt runs a single non-interactive turn using the same durable agent.
package cli
