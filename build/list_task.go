package main

import (
	"fmt"

	"github.com/goyek/goyek/v2"
)

// List is the default build task.
// It prints every registered task with its usage string for quick operator discovery.
var List = goyek.Define(goyek.Task{
	Name:  "list",
	Usage: "List all available tasks",
	Action: func(a *goyek.A) {
		fmt.Println("Available tasks:")
		fmt.Println()
		for _, task := range goyek.Tasks() {
			fmt.Printf("  %-20s %s\n", task.Name(), task.Usage())
		}
	},
})
