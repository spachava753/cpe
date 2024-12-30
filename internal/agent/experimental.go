package agent

import (
	"os"
	"strings"
)

// ExperimentalFlags represents the set of experimental features that can be enabled
type ExperimentalFlags struct {
	DisabledRelatedFiles bool
}

// ParseExperimentalFlags parses the CPE_EXPERIMENTAL environment variable and returns the enabled flags
func ParseExperimentalFlags() ExperimentalFlags {
	var flags ExperimentalFlags
	
	envVar := os.Getenv("CPE_EXPERIMENTAL")
	if envVar == "" {
		return flags
	}
	
	// Split by comma and trim spaces
	for _, flag := range strings.Split(envVar, ",") {
		flag = strings.TrimSpace(flag)
		switch flag {
		case "disabled_related_files":
			flags.DisabledRelatedFiles = true
		}
	}
	
	return flags
}