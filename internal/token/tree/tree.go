package tree

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/spachava753/gai"
)

// DirTreeNode represents a node in the directory tree with its token count
type DirTreeNode struct {
	Path     string
	Name     string
	IsDir    bool
	Count    uint
	Children []*DirTreeNode
}

// collectFilePaths collects all file paths in a directory recursively
func collectFilePaths(ctx context.Context, rootPath string) ([]string, error) {
	var files []string

	// Use fs.WalkDir with context for cancellable walk
	err := fs.WalkDir(os.DirFS(rootPath), ".", func(path string, d fs.DirEntry, err error) error {
		// Check for context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			return nil // Skip errors and continue
		}

		// Get the full path relative to the original root
		fullPath := filepath.Join(rootPath, path)

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Add file to the list
		files = append(files, fullPath)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking directory %s: %w", rootPath, err)
	}

	return files, nil
}

// BuildDirTree builds a directory tree with token counts
func BuildDirTree(
	ctx context.Context,
	rootPath string,
	tc gai.TokenCounter,
	progressWriter io.Writer,
) (*DirTreeNode, error) {
	// Get absolute path of the root
	absRootPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Step 1: Collect all file paths (fast operation)
	fmt.Fprintf(progressWriter, "Collecting files in %s...\n", rootPath)
	files, err := collectFilePaths(ctx, absRootPath) // Pass context here
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(progressWriter, "Found %d files to process\n", len(files))

	// Step 2: Count tokens in all files in parallel
	fileCounts, err := countFilesParallel(ctx, files, tc, true, progressWriter)
	if err != nil {
		return nil, err
	}

	// Step 3: Build the directory tree
	fmt.Fprintf(progressWriter, "Building directory tree...\n")

	tree := buildTreeFromCounts(absRootPath, fileCounts)

	fmt.Fprintf(progressWriter, "Done. Total token count: %d\n", tree.Count)

	return tree, nil
}

// buildTreeFromCounts builds a directory tree from file token counts
func buildTreeFromCounts(rootPath string, fileCounts map[string]uint) *DirTreeNode {
	// Create map of directory nodes
	dirMap := make(map[string]*DirTreeNode)

	// Create root node
	root := &DirTreeNode{
		Path:     rootPath,
		Name:     filepath.Base(rootPath),
		IsDir:    true,
		Count:    0,
		Children: []*DirTreeNode{},
	}
	dirMap[rootPath] = root

	// Make sure all directories in the tree exist
	for filePath, count := range fileCounts {
		// Get all parent directories
		dir := filepath.Dir(filePath)
		for dir != rootPath && dir != "." && dir != filepath.Dir(rootPath) {
			if _, exists := dirMap[dir]; !exists {
				name := filepath.Base(dir)
				node := &DirTreeNode{
					Path:     dir,
					Name:     name,
					IsDir:    true,
					Count:    0,
					Children: []*DirTreeNode{},
				}
				dirMap[dir] = node
			}
			dir = filepath.Dir(dir)
		}

		// Add file node to its parent directory
		parentDir := filepath.Dir(filePath)
		if parent, exists := dirMap[parentDir]; exists {
			fileName := filepath.Base(filePath)
			fileNode := &DirTreeNode{
				Path:     filePath,
				Name:     fileName,
				IsDir:    false,
				Count:    count,
				Children: nil,
			}
			parent.Children = append(parent.Children, fileNode)
		}
	}

	// Connect all directories in the tree
	for dirPath, node := range dirMap {
		if dirPath == rootPath {
			continue // Skip the root
		}

		parentDir := filepath.Dir(dirPath)
		if parent, exists := dirMap[parentDir]; exists {
			// Check if already a child
			isChild := false
			for _, child := range parent.Children {
				if child.Path == dirPath {
					isChild = true
					break
				}
			}

			if !isChild {
				parent.Children = append(parent.Children, node)
			}
		}
	}

	// Calculate directory counts by aggregating children
	calculateDirCounts(root)

	// Sort children alphabetically
	sortTree(root)

	return root
}

// calculateDirCounts recursively calculates directory token counts from their children
func calculateDirCounts(node *DirTreeNode) uint {
	if node == nil {
		return 0
	}

	// If it's a file, return its count
	if !node.IsDir {
		return node.Count
	}

	// For directories, sum all children
	var total uint
	for _, child := range node.Children {
		total += calculateDirCounts(child)
	}

	// Update node count
	node.Count = total

	return total
}

// sortTree sorts children of each node alphabetically, with directories first
func sortTree(node *DirTreeNode) {
	if node == nil || len(node.Children) == 0 {
		return
	}

	// Sort children
	sort.Slice(node.Children, func(i, j int) bool {
		// If both are directories or both are files, sort by name
		if node.Children[i].IsDir == node.Children[j].IsDir {
			return node.Children[i].Name < node.Children[j].Name
		}
		// Directories come before files
		return node.Children[i].IsDir
	})

	// Recursively sort children
	for _, child := range node.Children {
		sortTree(child)
	}
}

// PrintDirTree prints a directory tree with token counts
func PrintDirTree(node *DirTreeNode, indent string) {
	if node == nil {
		return
	}

	// Print current node
	if node.IsDir {
		fmt.Printf("%s📁 %s/ (%d tokens)\n", indent, node.Name, node.Count)
	} else {
		fmt.Printf("%s📄 %s (%d tokens)\n", indent, node.Name, node.Count)
	}

	// Print children with increased indentation
	childIndent := indent + "  "
	for _, child := range node.Children {
		PrintDirTree(child, childIndent)
	}
}
