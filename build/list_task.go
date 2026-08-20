package main

import (
	"fmt"

	"github.com/goyek/goyek/v2"
)

// Register the default task that prints every available task and its usage.
var _ = goyek.Define(goyek.Task{
	Name:  "list",
	Usage: "List all available tasks",
	Action: func(_ *goyek.A) {
		fmt.Println("Available tasks:")
		fmt.Println()
		for _, task := range goyek.Tasks() {
			fmt.Printf("  %-20s %s\n", task.Name(), task.Usage())
		}
	},
})
