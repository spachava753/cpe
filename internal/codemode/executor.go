package codemode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/printer"
	"go/token"
	goversion "go/version"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/imports"

	"github.com/spachava753/cpe/internal/mcp"
)

// mcpSDKVersion is the version of the MCP SDK to use in generated go.mod
const mcpSDKVersion = "v1.1.0"

// gracePeriod is the time to wait after sending SIGINT before sending SIGKILL
const gracePeriod = 5 * time.Second

// timeoutCancellationNoteTemplate is appended to output when executionTimeout triggers cancellation.
const timeoutCancellationNoteTemplate = "execution timed out after %d seconds; context was canceled because executionTimeout was reached."

// mcpSDKImport is the import path for the MCP SDK package
const mcpSDKImport = "github.com/modelcontextprotocol/go-sdk/mcp"

const spilledOutputFilePattern = "cpe-code-output-*.txt"

// ExecutionResult represents the outcome of code execution
type ExecutionResult struct {
	Output   string           // Combined stdout/stderr
	ExitCode int              // Exit code from the process
	Content  []mcpsdk.Content // Multimedia content returned from Run()
}

// ExecuteCodeOptions controls sandbox execution behavior.
type ExecuteCodeOptions struct {
	TimeoutSeconds       int
	LargeOutputCharLimit int
	LocalModulePaths     []string
}

type workspaceModule struct {
	ModulePath string
	Dir        string
}

var importsProcessMu sync.Mutex

// ExecuteCode creates a temporary sandbox, writes files, and executes LLM-generated Go code.
// It returns the execution result with combined output and exit code.
// The temp directory is cleaned up after execution.
//
// Error classification:
//   - nil error with ExitCode 0: successful execution
//   - RecoverableError: compilation errors, Run() errors (exit 1), panics (exit 2), timeouts
//   - FatalExecutionError: exit code 3 from fatalExit() in generated code
//   - Other errors: infrastructure failures (temp dir, file writes, etc.)
func ExecuteCode(ctx context.Context, servers []*mcp.MCPConn, llmCode string, opts ExecuteCodeOptions) (ExecutionResult, error) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "cpe-code-mode-*")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("creating temp directory: %w", err)
	}
	cleanupDir := tempDir
	if realTempDir, err := filepath.EvalSymlinks(tempDir); err == nil {
		tempDir = realTempDir
	}
	defer os.RemoveAll(cleanupDir)

	baseGoVersion, err := detectGoToolchainVersion(ctx)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("detecting go toolchain version: %w", err)
	}

	workspacePath, workspaceModules, workspaceGoVersion, err := prepareWorkspaceFile(tempDir, opts, baseGoVersion)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("preparing go workspace: %w", err)
	}

	// Generate and write go.mod using the resolved workspace go version.
	goMod := generateGoMod(workspaceGoVersion)
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return ExecutionResult{}, fmt.Errorf("writing go.mod: %w", err)
	}
	if realWorkspacePath, err := filepath.EvalSymlinks(workspacePath); err == nil {
		workspacePath = realWorkspacePath
	}
	envOverrides := map[string]string{"GOWORK": workspacePath}

	if err := applyWorkspaceModuleReplacements(ctx, tempDir, envOverrides, workspaceModules); err != nil {
		return ExecutionResult{}, fmt.Errorf("configuring workspace module replacements: %w", err)
	}

	// Generate and write main.go
	mainGo, err := GenerateMainGo(servers, filepath.Join(tempDir, "content.json"))
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("generating main.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(mainGo), 0o644); err != nil {
		return ExecutionResult{}, fmt.Errorf("writing main.go: %w", err)
	}

	// Write run.go (LLM-generated code)
	if err := os.WriteFile(filepath.Join(tempDir, "run.go"), []byte(llmCode), 0o644); err != nil {
		return ExecutionResult{}, fmt.Errorf("writing run.go: %w", err)
	}

	// Auto-correct imports
	importNote := autoCorrectImports(ctx, tempDir, "run.go", envOverrides)

	// Run go mod tidy
	tidyResult, err := runCommand(ctx, tempDir, envOverrides, "go", "mod", "tidy")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("running go mod tidy: %w", err)
	}
	tidyResult.Output += importNote
	tidyResult = maybeSpillLargeOutput(tidyResult, opts.LargeOutputCharLimit)
	if tidyResult.ExitCode != 0 {
		return ExecutionResult{
			Output:   tidyResult.Output,
			ExitCode: tidyResult.ExitCode,
		}, RecoverableError{Output: tidyResult.Output, ExitCode: tidyResult.ExitCode}
	}

	// Build the binary to get accurate exit codes (go run masks them)
	binaryPath := filepath.Join(tempDir, "program")
	buildResult, err := runCommand(ctx, tempDir, envOverrides, "go", "build", "-o", binaryPath, ".")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("running go build: %w", err)
	}
	buildResult.Output += importNote
	buildResult = maybeSpillLargeOutput(buildResult, opts.LargeOutputCharLimit)
	if buildResult.ExitCode != 0 {
		return ExecutionResult{
			Output:   buildResult.Output,
			ExitCode: buildResult.ExitCode,
		}, RecoverableError{Output: buildResult.Output, ExitCode: buildResult.ExitCode}
	}

	// Execute the built binary with timeout and graceful shutdown
	result, err := runProgramWithTimeout(ctx, binaryPath, opts.TimeoutSeconds, envOverrides)
	result.Output += importNote
	result = maybeSpillLargeOutput(result, opts.LargeOutputCharLimit)
	if err != nil {
		return result, err
	}

	// Read content.json on successful execution (exit code 0)
	if result.ExitCode == 0 {
		contentPath := filepath.Join(tempDir, "content.json")
		if _, err := os.Stat(contentPath); err == nil {
			data, err := os.ReadFile(contentPath)
			if err != nil {
				return result, fmt.Errorf("reading content file: %w", err)
			}
			content, err := unmarshalContent(data)
			if err != nil {
				return result, fmt.Errorf("deserializing content: %w", err)
			}
			result.Content = content
		}
	}

	return result, classifyExitCode(result)
}

// generateGoMod creates the go.mod file contents.
func generateGoMod(goVersion string) string {
	return fmt.Sprintf(`module cpe-code-execution

go %s

require github.com/modelcontextprotocol/go-sdk %s
`, goVersion, mcpSDKVersion)
}

func prepareWorkspaceFile(tempModuleDir string, opts ExecuteCodeOptions, defaultGoVersion string) (string, []workspaceModule, string, error) {
	workspaceModulePaths := append([]string{tempModuleDir}, opts.LocalModulePaths...)

	normalizedPaths, err := normalizeWorkspaceModulePaths(workspaceModulePaths)
	if err != nil {
		return "", nil, "", err
	}

	workspaceGoVersion, err := normalizeGoDirectiveVersion(defaultGoVersion)
	if err != nil {
		return "", nil, "", fmt.Errorf("normalizing default workspace go version: %w", err)
	}

	modules := make([]workspaceModule, 0, len(normalizedPaths))
	for _, modulePath := range normalizedPaths {
		if modulePath == normalizedPaths[0] {
			continue // temp module generated in this execution always has go.mod
		}
		if err := validateWorkspaceModulePath(modulePath); err != nil {
			return "", nil, "", err
		}

		moduleName, moduleGoVersion, err := readWorkspaceModuleInfo(modulePath)
		if err != nil {
			return "", nil, "", err
		}
		if moduleGoVersion != "" && compareGoDirectiveVersions(moduleGoVersion, workspaceGoVersion) > 0 {
			workspaceGoVersion = moduleGoVersion
		}
		modules = append(modules, workspaceModule{ModulePath: moduleName, Dir: modulePath})
	}

	workspaceFile := &modfile.WorkFile{Syntax: &modfile.FileSyntax{Name: "go.work"}}
	if err := workspaceFile.AddGoStmt(workspaceGoVersion); err != nil {
		return "", nil, "", fmt.Errorf("adding go directive to workspace: %w", err)
	}
	for _, modulePath := range normalizedPaths {
		if err := workspaceFile.AddUse(modulePath, ""); err != nil {
			return "", nil, "", fmt.Errorf("adding workspace use path %q: %w", modulePath, err)
		}
	}
	workspaceFile.SortBlocks()
	workspaceFile.Cleanup()

	workspacePath := filepath.Join(tempModuleDir, "go.work")
	if err := os.WriteFile(workspacePath, modfile.Format(workspaceFile.Syntax), 0o644); err != nil {
		return "", nil, "", fmt.Errorf("writing workspace file: %w", err)
	}

	return workspacePath, modules, workspaceGoVersion, nil
}

// normalizeWorkspaceModulePaths resolves, cleans, and deduplicates module paths.
// Unlike config validation (which errors on duplicates), this function silently
// deduplicates because it merges multiple sources: the temp module and
// user-configured localModulePaths.
func normalizeWorkspaceModulePaths(paths []string) ([]string, error) {
	normalized := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))

	for i, rawPath := range paths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			return nil, fmt.Errorf("workspace module path at index %d must not be empty", i)
		}

		absPath, err := filepath.Abs(trimmed)
		if err != nil {
			return nil, fmt.Errorf("resolving absolute path for workspace module %q: %w", trimmed, err)
		}
		cleanPath := filepath.Clean(absPath)
		if realPath, err := filepath.EvalSymlinks(cleanPath); err == nil {
			cleanPath = filepath.Clean(realPath)
		}

		if _, exists := seen[cleanPath]; exists {
			continue
		}
		seen[cleanPath] = struct{}{}
		normalized = append(normalized, cleanPath)
	}

	return normalized, nil
}

func validateWorkspaceModulePath(modulePath string) error {
	moduleStat, err := os.Stat(modulePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("workspace module path does not exist: %s", modulePath)
		}
		return fmt.Errorf("checking workspace module path %q: %w", modulePath, err)
	}
	if !moduleStat.IsDir() {
		return fmt.Errorf("workspace module path is not a directory: %s", modulePath)
	}

	goModPath := filepath.Join(modulePath, "go.mod")
	goModStat, err := os.Stat(goModPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("workspace module path is missing go.mod: %s", modulePath)
		}
		return fmt.Errorf("checking go.mod for workspace module %q: %w", modulePath, err)
	}
	if goModStat.IsDir() {
		return fmt.Errorf("workspace module has directory instead of go.mod file: %s", goModPath)
	}

	return nil
}

func readWorkspaceModuleInfo(moduleDir string) (modulePath, goVersion string, err error) {
	goModPath := filepath.Join(moduleDir, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", "", fmt.Errorf("reading go.mod for workspace module %q: %w", moduleDir, err)
	}

	parsed, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return "", "", fmt.Errorf("parsing go.mod for workspace module %q: %w", moduleDir, err)
	}
	if parsed.Module == nil || strings.TrimSpace(parsed.Module.Mod.Path) == "" {
		return "", "", fmt.Errorf("workspace module %q has no module directive in go.mod", moduleDir)
	}

	moduleGoVersion := ""
	if parsed.Go != nil {
		moduleGoVersion, err = normalizeGoDirectiveVersion(parsed.Go.Version)
		if err != nil {
			return "", "", fmt.Errorf("parsing go directive for workspace module %q: %w", moduleDir, err)
		}
	}

	return parsed.Module.Mod.Path, moduleGoVersion, nil
}

func normalizeGoDirectiveVersion(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("go version must not be empty")
	}
	if strings.HasPrefix(trimmed, "go") {
		trimmed = strings.TrimPrefix(trimmed, "go")
	}
	if strings.HasPrefix(trimmed, "v") {
		trimmed = strings.TrimPrefix(trimmed, "v")
	}

	prefixed := "go" + trimmed
	if !goversion.IsValid(prefixed) {
		return "", fmt.Errorf("invalid go version %q", raw)
	}

	return strings.TrimPrefix(prefixed, "go"), nil
}

func compareGoDirectiveVersions(left, right string) int {
	return goversion.Compare("go"+left, "go"+right)
}

func detectGoToolchainVersion(ctx context.Context) (string, error) {
	result, err := runCommand(ctx, "", nil, "go", "env", "GOVERSION")
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", errors.New(strings.TrimSpace(result.Output))
	}

	version, err := normalizeGoDirectiveVersion(result.Output)
	if err != nil {
		return "", err
	}
	return version, nil
}

func applyWorkspaceModuleReplacements(ctx context.Context, dir string, envOverrides map[string]string, modules []workspaceModule) error {
	if len(modules) == 0 {
		return nil
	}

	for _, module := range modules {
		editArg := fmt.Sprintf("%s=%s", module.ModulePath, module.Dir)
		result, err := runCommand(ctx, dir, envOverrides, "go", "mod", "edit", "-replace", editArg)
		if err != nil {
			return fmt.Errorf("running go mod edit for %q: %w", module.ModulePath, err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("go mod edit -replace %s failed: %s", editArg, strings.TrimSpace(result.Output))
		}
	}

	return nil
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	merged := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		merged[parts[0]] = parts[1]
	}
	for key, value := range overrides {
		merged[key] = value
	}

	result := make([]string, 0, len(merged))
	for key, value := range merged {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}

// runCommand executes a command in the given directory and returns the result.
func runCommand(ctx context.Context, dir string, envOverrides map[string]string, name string, args ...string) (ExecutionResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = mergeEnv(os.Environ(), envOverrides)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()

	result := ExecutionResult{
		Output:   output.String(),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., command not found, context cancelled)
			return result, err
		}
	}

	return result, nil
}

// runProgramWithTimeout executes a binary with timeout enforcement and graceful shutdown.
// On timeout or context cancellation, it sends SIGINT and waits gracePeriod for the process
// to exit gracefully before sending SIGKILL.
func runProgramWithTimeout(ctx context.Context, binaryPath string, timeoutSecs int, envOverrides map[string]string) (ExecutionResult, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, binaryPath)
	cmd.Env = mergeEnv(os.Environ(), envOverrides)

	// Custom cancel: send SIGINT for graceful shutdown instead of default SIGKILL.
	// Return os.ErrProcessDone to suppress context error when process exits cleanly.
	cmd.Cancel = func() error {
		cmd.Process.Signal(syscall.SIGINT)
		return os.ErrProcessDone
	}

	// Grace period: if process doesn't exit after SIGINT, Go sends SIGKILL
	cmd.WaitDelay = gracePeriod

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()

	result := ExecutionResult{
		Output:   output.String(),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., binary not found)
			return result, err
		}
	}

	if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
		result.Output = appendTimeoutCancellationNote(result.Output, timeoutSecs)
	}

	return result, nil
}

func appendTimeoutCancellationNote(output string, timeoutSecs int) string {
	note := fmt.Sprintf(timeoutCancellationNoteTemplate, timeoutSecs)
	if output == "" {
		return note
	}
	if strings.HasSuffix(output, "\n") {
		return output + note
	}
	return output + "\n" + note
}

func maybeSpillLargeOutput(result ExecutionResult, charLimit int) ExecutionResult {
	if charLimit <= 0 || result.Output == "" {
		return result
	}

	outputChars := utf8.RuneCountInString(result.Output)
	if outputChars <= charLimit {
		return result
	}

	preview := firstNChars(result.Output, charLimit)
	spillPath, spillErr := spillOutputToDisk(result.Output)
	result.Output = formatSpilledOutputMessage(outputChars, charLimit, preview, spillPath, spillErr)
	return result
}

func spillOutputToDisk(output string) (string, error) {
	f, err := os.CreateTemp("", spilledOutputFilePattern)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := f.WriteString(output); err != nil {
		return "", err
	}

	return f.Name(), nil
}

func firstNChars(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func formatSpilledOutputMessage(totalChars, charLimit int, preview, spillPath string, spillErr error) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("WARNING: tool result exceeded character limit (%d characters > %d).\n", totalChars, charLimit))

	if spillErr != nil {
		b.WriteString(fmt.Sprintf("Failed to persist full output to disk: %v\n", spillErr))
	} else {
		b.WriteString(fmt.Sprintf("Full output stored at: %s\n", spillPath))
	}

	b.WriteString(fmt.Sprintf("\nPreview (first %d characters):\n", charLimit))
	b.WriteString("---\n")
	b.WriteString(preview)
	if preview != "" && !strings.HasSuffix(preview, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("---\n")

	if spillErr == nil {
		b.WriteString("\nThe preview is truncated at the configured character limit. Read the file above for the remaining output.")
	} else {
		b.WriteString("\nThe preview is truncated at the configured character limit. Full output could not be persisted; re-run with more focused output.")
	}

	return b.String()
}

// classifyExitCode returns an appropriate error based on the execution result's exit code.
func classifyExitCode(result ExecutionResult) error {
	switch result.ExitCode {
	case 0:
		return nil
	case 1, 2:
		// Exit 1: Run() returned error; Exit 2: panic - both recoverable
		return RecoverableError{Output: result.Output, ExitCode: result.ExitCode}
	case 3:
		// Exit 3: fatalExit() called - unrecoverable
		return FatalExecutionError{Output: result.Output}
	default:
		// Other non-zero codes (e.g., -1 from SIGKILL) are recoverable
		return RecoverableError{Output: result.Output, ExitCode: result.ExitCode}
	}
}

// autoCorrectImports runs goimports (via golang.org/x/tools/imports) on the file
// and returns a notification message listing added/removed packages.
func autoCorrectImports(ctx context.Context, dir, filename string, envOverrides map[string]string) string {
	_ = ctx
	filePath := filepath.Join(dir, filename)
	orig, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	// Ensure correct mcp import before goimports to prevent wrong package resolution
	preprocessed := ensureMCPImport(orig)
	origImports := extractImports(orig)

	importsProcessMu.Lock()
	defer importsProcessMu.Unlock()
	restoreEnv := applyEnvOverrides(envOverrides)
	defer restoreEnv()
	newContent, err := imports.Process(filePath, preprocessed, nil)
	if err != nil {
		// If processing fails (e.g. syntax errors), let the compiler catch it
		return ""
	}

	if bytes.Equal(orig, newContent) {
		return ""
	}

	if err := os.WriteFile(filePath, newContent, 0o644); err != nil {
		return ""
	}

	newImports := extractImports(newContent)
	return formatImportChanges(filename, origImports, newImports)
}

func applyEnvOverrides(overrides map[string]string) func() {
	if len(overrides) == 0 {
		return func() {}
	}

	type envValue struct {
		value  string
		exists bool
	}
	previous := make(map[string]envValue, len(overrides))

	for key, value := range overrides {
		current, exists := os.LookupEnv(key)
		previous[key] = envValue{value: current, exists: exists}
		_ = os.Setenv(key, value)
	}

	return func() {
		for key, prev := range previous {
			if prev.exists {
				_ = os.Setenv(key, prev.value)
				continue
			}
			_ = os.Unsetenv(key)
		}
	}
}

// extractImports parses Go source and returns a set of import paths.
func extractImports(src []byte) map[string]bool {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil {
		return nil
	}

	imps := make(map[string]bool)
	for _, imp := range f.Imports {
		// Remove quotes from import path
		path := strings.Trim(imp.Path.Value, "`\"")
		imps[path] = true
	}
	return imps
}

// contentTypeWrapper is used to peek at the type field during deserialization
type contentTypeWrapper struct {
	Type string `json:"type"`
}

// unmarshalContent deserializes a JSON array of MCP content items.
// It uses a two-phase approach: first peek at the type field, then unmarshal to the concrete type.
func unmarshalContent(data []byte) ([]mcpsdk.Content, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(data, &rawItems); err != nil {
		return nil, fmt.Errorf("unmarshaling content array: %w", err)
	}

	result := make([]mcpsdk.Content, 0, len(rawItems))
	for i, raw := range rawItems {
		var wrapper contentTypeWrapper
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return nil, fmt.Errorf("peeking type for item %d: %w", i, err)
		}

		var content mcpsdk.Content
		switch wrapper.Type {
		case "text":
			var tc mcpsdk.TextContent
			if err := json.Unmarshal(raw, &tc); err != nil {
				return nil, fmt.Errorf("unmarshaling text content at index %d: %w", i, err)
			}
			content = &tc
		case "image":
			var ic mcpsdk.ImageContent
			if err := json.Unmarshal(raw, &ic); err != nil {
				return nil, fmt.Errorf("unmarshaling image content at index %d: %w", i, err)
			}
			content = &ic
		case "audio":
			var ac mcpsdk.AudioContent
			if err := json.Unmarshal(raw, &ac); err != nil {
				return nil, fmt.Errorf("unmarshaling audio content at index %d: %w", i, err)
			}
			content = &ac
		default:
			return nil, fmt.Errorf("unknown content type %q at index %d", wrapper.Type, i)
		}
		result = append(result, content)
	}

	return result, nil
}

// ensureMCPImport adds the MCP SDK import if not already present.
// This prevents goimports from resolving mcp to the wrong package.
// The import will be removed by goimports if not actually used.
func ensureMCPImport(src []byte) []byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src
	}

	astutil.AddImport(fset, f, mcpSDKImport)

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, f); err != nil {
		return src
	}

	return buf.Bytes()
}

// formatImportChanges generates a message describing which imports were added/removed.
func formatImportChanges(filename string, origImports, newImports map[string]bool) string {
	var added, removed []string

	for pkg := range newImports {
		if !origImports[pkg] {
			added = append(added, pkg)
		}
	}
	for pkg := range origImports {
		if !newImports[pkg] {
			removed = append(removed, pkg)
		}
	}

	if len(added) == 0 && len(removed) == 0 {
		return ""
	}

	sort.Strings(added)
	sort.Strings(removed)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n\nNote: Imports in %s were auto-corrected.", filename))
	if len(added) > 0 {
		sb.WriteString(fmt.Sprintf("\n  Added: %s", strings.Join(added, ", ")))
	}
	if len(removed) > 0 {
		sb.WriteString(fmt.Sprintf("\n  Removed: %s", strings.Join(removed, ", ")))
	}
	return sb.String()
}
