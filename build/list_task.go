package main

import (
	"fmt"

	"github.com/goyek/goyek/v2"
)

// List prints all registered tasks with their usage descriptions
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
