package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// SystemInfo contains information about the current system environment
type SystemInfo struct {
	CurrentDate string
	WorkingDir  string
	OS          string
	IsGitRepo   bool
}

// GetSystemInfo gathers current system information
func GetSystemInfo() (*SystemInfo, error) {
	// Get current working directory
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Check if we're in a git repository by looking for .git directory
	isGitRepo := false
	if _, err := os.Stat(filepath.Join(wd, ".git")); err == nil {
		isGitRepo = true
	}

	return &SystemInfo{
		CurrentDate: time.Now().Format(time.RFC3339),
		WorkingDir:  wd,
		OS:          runtime.GOOS,
		IsGitRepo:   isGitRepo,
	}, nil
}

// FormatSystemInfo formats the system information as a string
func (si *SystemInfo) FormatSystemInfo() string {
	gitStatus := "not a git repository"
	if si.IsGitRepo {
		gitStatus = "git repository"
	}

	return fmt.Sprintf(`System Information:
- Current Date: %s
- Working Directory: %s
- Operating System: %s
- Repository: %s
`, si.CurrentDate, si.WorkingDir, si.OS, gitStatus)
}