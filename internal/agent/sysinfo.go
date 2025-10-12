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

	"github.com/spachava753/cpe/internal/config"
)

// SystemInfo contains information about the current system environment
type SystemInfo struct {
	CurrentDate string
	WorkingDir  string
	OS          string
	IsGitRepo   bool

	// Additional fields
	Username    string
	Hostname    string
	GoVersion   string
	CurrentTime string
	Timezone    string

	// Git-specific fields
	GitBranch        string
	GitLatestCommit  string
	GitCommitMessage string
	GitHasChanges    bool

	// Model information
	ModelRef         string
	ModelDisplayName string
	ModelID          string
	ModelType        string
}

// GetSystemInfoWithModel gathers current system information with optional model information
func GetSystemInfoWithModel(model *config.Model) (*SystemInfo, error) {
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
	now := time.Now()
	_, offset := now.Zone()
	offsetHours := offset / 3600
	offsetSign := "+"
	if offsetHours < 0 {
		offsetSign = "-"
		offsetHours = -offsetHours
	}

	sysInfo := &SystemInfo{
		CurrentDate: now.Format(time.DateOnly),
		CurrentTime: now.Format("15:04:05"),
		Timezone:    fmt.Sprintf("%s (UTC%s%d)", now.Location().String(), offsetSign, offsetHours),
		WorkingDir:  wd,
		OS:          runtime.GOOS,
		IsGitRepo:   isGitRepo,
	}

	// Add model information if provided
	if model != nil {
		sysInfo.ModelRef = model.Ref
		sysInfo.ModelDisplayName = model.DisplayName
		sysInfo.ModelID = model.ID
		sysInfo.ModelType = model.Type
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

// ExecuteTemplateString renders a template string with system info data
func (si *SystemInfo) ExecuteTemplateString(templateStr string) (string, error) {
	tmpl, err := template.New("sysinfo").Funcs(createTemplateFuncMap()).Parse(templateStr)
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
