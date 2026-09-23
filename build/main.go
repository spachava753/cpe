package main

import (
	"flag"

	"github.com/goyek/goyek/v2"
)

// Flags consumed by the lint task.
var (
	lintFix     = flag.Bool("lint-fix", false, "Auto-fix linting issues")
	lintVerbose = flag.Bool("lint-verbose", false, "Verbose linting output")
)

// main parses global build-task flags and dispatches requested goyek tasks.
// With no task arguments, it runs "list" to show operator-facing task help.
func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		args = []string{"list"}
	}
	goyek.Main(args)
}
