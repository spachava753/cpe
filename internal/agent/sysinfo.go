package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"
)

// SystemInfo contains information about the current system environment
type SystemInfo struct {
	CurrentDate string
	WorkingDir  string
	OS          string
	IsGitRepo   bool

	// Additional fields
	Username         string
	Hostname         string
	GoVersion        string

	// Git-specific fields
	GitBranch        string
	GitLatestCommit  string
	GitCommitMessage string
	GitHasChanges    bool
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

	// Create the base SystemInfo
	sysInfo := &SystemInfo{
		CurrentDate: time.Now().Format(time.DateOnly),
		WorkingDir:  wd,
		OS:          runtime.GOOS,
		IsGitRepo:   isGitRepo,
	}

	// Try to get username
	if currentUser, err := user.Current(); err == nil {
		sysInfo.Username = currentUser.Username
	}

	// Try to get hostname
	if hostname, err := os.Hostname(); err == nil {
		sysInfo.Hostname = hostname
	}

	// Try to get Go version
	if goVersionCmd := exec.Command("go", "version"); goVersionCmd != nil {
		if output, err := goVersionCmd.Output(); err == nil {
			sysInfo.GoVersion = strings.TrimSpace(string(output))
		}
	}

	// If in a git repo, get git information
	if isGitRepo {
		// Get current branch
		if branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD"); branchCmd != nil {
			if output, err := branchCmd.Output(); err == nil {
				sysInfo.GitBranch = strings.TrimSpace(string(output))
			}
		}

		// Get latest commit hash
		if commitCmd := exec.Command("git", "rev-parse", "--short", "HEAD"); commitCmd != nil {
			if output, err := commitCmd.Output(); err == nil {
				sysInfo.GitLatestCommit = strings.TrimSpace(string(output))
			}
		}

		// Get latest commit message
		if msgCmd := exec.Command("git", "log", "-1", "--pretty=%B"); msgCmd != nil {
			if output, err := msgCmd.Output(); err == nil {
				sysInfo.GitCommitMessage = strings.TrimSpace(string(output))
			}
		}

		// Check if there are uncommitted changes
		if statusCmd := exec.Command("git", "status", "--porcelain"); statusCmd != nil {
			if output, err := statusCmd.Output(); err == nil {
				sysInfo.GitHasChanges = len(output) > 0
			}
		}
	}

	return sysInfo, nil
}

// String implements fmt.Stringer
func (si *SystemInfo) String() string {
	// Create the basic system info string
	info := fmt.Sprintf(`System Information:
- Current Date: %s
- Working Directory: %s
- Operating System: %s
- Is Git Repository: %v
`, si.CurrentDate, si.WorkingDir, si.OS, si.IsGitRepo)

	// Add git info if available
	if si.IsGitRepo && si.GitBranch != "" {
		gitInfo := fmt.Sprintf(`- Git Branch: %s
- Latest Commit: %s
- Commit Message: %s
- Has Uncommitted Changes: %v
`, si.GitBranch, si.GitLatestCommit, si.GitCommitMessage, si.GitHasChanges)
		info += gitInfo
	}

	return info
}

// ExecuteTemplateString renders a template string with system info data
func (si *SystemInfo) ExecuteTemplateString(templateStr string) (string, error) {
	tmpl, err := template.New("sysinfo").Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template string: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, si); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// ExecuteTemplate renders a template file with system info data
func (si *SystemInfo) ExecuteTemplate(templatePath string) (string, error) {
	// Read the template file
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read template file: %w", err)
	}

	// Use the string version with the file contents
	return si.ExecuteTemplateString(string(tmplContent))
}
