package parser

import (
	"fmt"
	"regexp"
	"strings"
)

type Modification interface {
	Type() string
}

type ModifyCode struct {
	Path          string
	Modifications []struct {
		Search  string
		Replace string
	}
	Explanation string
}

func (m ModifyCode) Type() string {
	return "ModifyCode"
}

type RemoveFile struct {
	Path        string
	Explanation string
}

func (r RemoveFile) Type() string {
	return "RemoveFile"
}

type CreateFile struct {
	Path        string
	Content     string
	Explanation string
}

func (c CreateFile) Type() string {
	return "CreateFile"
}

type RenameFile struct {
	OldPath     string
	NewPath     string
	Explanation string
}

func (r RenameFile) Type() string {
	return "RenameFile"
}

type MoveFile struct {
	OldPath     string
	NewPath     string
	Explanation string
}

func (m MoveFile) Type() string {
	return "MoveFile"
}

type CreateDirectory struct {
	Path        string
	Explanation string
}

func (c CreateDirectory) Type() string {
	return "CreateDirectory"
}

func ParseModifications(input string) ([]Modification, error) {
	var modifications []Modification

	// Define regex patterns for each modification type
	modifyCodePattern := regexp.MustCompile(`(?s)<modify_code>(.*?)</modify_code>`)
	removeFilePattern := regexp.MustCompile(`(?s)<remove_file>(.*?)</remove_file>`)
	createFilePattern := regexp.MustCompile(`(?s)<create_file>(.*?)</create_file>`)
	renameFilePattern := regexp.MustCompile(`(?s)<rename_file>(.*?)</rename_file>`)
	moveFilePattern := regexp.MustCompile(`(?s)<move_file>(.*?)</move_file>`)
	createDirectoryPattern := regexp.MustCompile(`(?s)<create_directory>(.*?)</create_directory>`)

	// Parse modify_code
	modifyCodeMatches := modifyCodePattern.FindAllStringSubmatch(input, -1)
	for _, match := range modifyCodeMatches {
		mod, err := parseModifyCode(match[1])
		if err != nil {
			return nil, err
		}
		modifications = append(modifications, mod)
	}

	// Parse remove_file
	removeFileMatches := removeFilePattern.FindAllStringSubmatch(input, -1)
	for _, match := range removeFileMatches {
		mod, err := parseRemoveFile(match[1])
		if err != nil {
			return nil, err
		}
		modifications = append(modifications, mod)
	}

	// Parse create_file
	createFileMatches := createFilePattern.FindAllStringSubmatch(input, -1)
	for _, match := range createFileMatches {
		mod, err := parseCreateFile(match[1])
		if err != nil {
			return nil, err
		}
		modifications = append(modifications, mod)
	}

	// Parse rename_file
	renameFileMatches := renameFilePattern.FindAllStringSubmatch(input, -1)
	for _, match := range renameFileMatches {
		mod, err := parseRenameFile(match[1])
		if err != nil {
			return nil, err
		}
		modifications = append(modifications, mod)
	}

	// Parse move_file
	moveFileMatches := moveFilePattern.FindAllStringSubmatch(input, -1)
	for _, match := range moveFileMatches {
		mod, err := parseMoveFile(match[1])
		if err != nil {
			return nil, err
		}
		modifications = append(modifications, mod)
	}

	// Parse create_directory
	createDirectoryMatches := createDirectoryPattern.FindAllStringSubmatch(input, -1)
	for _, match := range createDirectoryMatches {
		mod, err := parseCreateDirectory(match[1])
		if err != nil {
			return nil, err
		}
		modifications = append(modifications, mod)
	}

	return modifications, nil
}

func parseModifyCode(input string) (ModifyCode, error) {
	pathPattern := regexp.MustCompile(`<path>(.*?)</path>`)
	modificationPattern := regexp.MustCompile(`(?s)<modification>.*?<search>(.*?)</search>.*?<replace>(.*?)</replace>.*?</modification>`)
	incompleteModificationPattern := regexp.MustCompile(`(?s)<modification>.*?<search>.*?</search>.*?</modification>`)
	explanationPattern := regexp.MustCompile(`(?s)<explanation>(.*?)</explanation>`)

	pathMatch := pathPattern.FindStringSubmatch(input)
	if len(pathMatch) < 2 {
		return ModifyCode{}, fmt.Errorf("path not found in modify_code")
	}
	if strings.TrimSpace(pathMatch[1]) == "" {
		return ModifyCode{}, fmt.Errorf("empty path in modify_code")
	}

	// Check for incomplete modifications
	incompleteModifications := incompleteModificationPattern.FindAllString(input, -1)
	completeModifications := modificationPattern.FindAllString(input, -1)
	if len(incompleteModifications) > len(completeModifications) {
		return ModifyCode{}, fmt.Errorf("incomplete modification found in modify_code")
	}

	modificationMatches := modificationPattern.FindAllStringSubmatch(input, -1)
	var modifications []struct {
		Search  string
		Replace string
	}
	for _, match := range modificationMatches {
		modifications = append(modifications, struct {
			Search  string
			Replace string
		}{
			Search:  strings.TrimSpace(match[1]),
			Replace: strings.TrimSpace(match[2]),
		})
	}

	explanationMatch := explanationPattern.FindStringSubmatch(input)
	explanation := ""
	if len(explanationMatch) >= 2 {
		explanation = strings.TrimSpace(explanationMatch[1])
	}
	if explanation == "" {
		fmt.Printf("Warning: Empty explanation found in %s\n", ModifyCode{}.Type())
	}

	return ModifyCode{
		Path:          strings.TrimSpace(pathMatch[1]),
		Modifications: modifications,
		Explanation:   explanation,
	}, nil
}

func parseRemoveFile(input string) (RemoveFile, error) {
	pathPattern := regexp.MustCompile(`<path>(.*?)</path>`)
	explanationPattern := regexp.MustCompile(`(?s)<explanation>(.*?)</explanation>`)

	pathMatch := pathPattern.FindStringSubmatch(input)
	if len(pathMatch) < 2 {
		return RemoveFile{}, fmt.Errorf("path not found in remove_file")
	}
	if strings.TrimSpace(pathMatch[1]) == "" {
		return RemoveFile{}, fmt.Errorf("empty path in remove_file")
	}

	explanationMatch := explanationPattern.FindStringSubmatch(input)
	explanation := ""
	if len(explanationMatch) >= 2 {
		explanation = strings.TrimSpace(explanationMatch[1])
	}
	if explanation == "" {
		fmt.Printf("Warning: Empty explanation found in %s\n", RemoveFile{}.Type())
	}

	return RemoveFile{
		Path:        strings.TrimSpace(pathMatch[1]),
		Explanation: explanation,
	}, nil
}

func parseCreateFile(input string) (CreateFile, error) {
	pathPattern := regexp.MustCompile(`<path>(.*?)</path>`)
	contentPattern := regexp.MustCompile(`(?s)<content>(.*?)</content>`)
	explanationPattern := regexp.MustCompile(`(?s)<explanation>(.*?)</explanation>`)

	pathMatch := pathPattern.FindStringSubmatch(input)
	if len(pathMatch) < 2 {
		return CreateFile{}, fmt.Errorf("path not found in create_file")
	}
	if strings.TrimSpace(pathMatch[1]) == "" {
		return CreateFile{}, fmt.Errorf("empty path in create_file")
	}

	contentMatch := contentPattern.FindStringSubmatch(input)
	if len(contentMatch) < 2 {
		return CreateFile{}, fmt.Errorf("content not found in create_file")
	}

	explanationMatch := explanationPattern.FindStringSubmatch(input)
	explanation := ""
	if len(explanationMatch) >= 2 {
		explanation = strings.TrimSpace(explanationMatch[1])
	}
	if explanation == "" {
		fmt.Printf("Warning: Empty explanation found in %s\n", CreateFile{}.Type())
	}

	return CreateFile{
		Path:        strings.TrimSpace(pathMatch[1]),
		Content:     strings.TrimSpace(contentMatch[1]),
		Explanation: explanation,
	}, nil
}

func parseRenameFile(input string) (RenameFile, error) {
	oldPathPattern := regexp.MustCompile(`<old_path>(.*?)</old_path>`)
	newPathPattern := regexp.MustCompile(`<new_path>(.*?)</new_path>`)
	explanationPattern := regexp.MustCompile(`(?s)<explanation>(.*?)</explanation>`)

	oldPathMatch := oldPathPattern.FindStringSubmatch(input)
	if len(oldPathMatch) < 2 {
		return RenameFile{}, fmt.Errorf("old_path not found in rename_file")
	}
	if strings.TrimSpace(oldPathMatch[1]) == "" {
		return RenameFile{}, fmt.Errorf("empty old_path in rename_file")
	}

	newPathMatch := newPathPattern.FindStringSubmatch(input)
	if len(newPathMatch) < 2 {
		return RenameFile{}, fmt.Errorf("new_path not found in rename_file")
	}
	if strings.TrimSpace(newPathMatch[1]) == "" {
		return RenameFile{}, fmt.Errorf("empty new_path in rename_file")
	}

	explanationMatch := explanationPattern.FindStringSubmatch(input)
	explanation := ""
	if len(explanationMatch) >= 2 {
		explanation = strings.TrimSpace(explanationMatch[1])
	}
	if explanation == "" {
		fmt.Printf("Warning: Empty explanation found in %s\n", RenameFile{}.Type())
	}

	return RenameFile{
		OldPath:     strings.TrimSpace(oldPathMatch[1]),
		NewPath:     strings.TrimSpace(newPathMatch[1]),
		Explanation: explanation,
	}, nil
}

func parseMoveFile(input string) (MoveFile, error) {
	oldPathPattern := regexp.MustCompile(`<old_path>(.*?)</old_path>`)
	newPathPattern := regexp.MustCompile(`<new_path>(.*?)</new_path>`)
	explanationPattern := regexp.MustCompile(`(?s)<explanation>(.*?)</explanation>`)

	oldPathMatch := oldPathPattern.FindStringSubmatch(input)
	if len(oldPathMatch) < 2 {
		return MoveFile{}, fmt.Errorf("old_path not found in move_file")
	}
	if strings.TrimSpace(oldPathMatch[1]) == "" {
		return MoveFile{}, fmt.Errorf("empty old_path in move_file")
	}

	newPathMatch := newPathPattern.FindStringSubmatch(input)
	if len(newPathMatch) < 2 {
		return MoveFile{}, fmt.Errorf("new_path not found in move_file")
	}
	if strings.TrimSpace(newPathMatch[1]) == "" {
		return MoveFile{}, fmt.Errorf("empty new_path in move_file")
	}

	explanationMatch := explanationPattern.FindStringSubmatch(input)
	explanation := ""
	if len(explanationMatch) >= 2 {
		explanation = strings.TrimSpace(explanationMatch[1])
	}
	if explanation == "" {
		fmt.Printf("Warning: Empty explanation found in %s\n", MoveFile{}.Type())
	}

	return MoveFile{
		OldPath:     strings.TrimSpace(oldPathMatch[1]),
		NewPath:     strings.TrimSpace(newPathMatch[1]),
		Explanation: explanation,
	}, nil
}

func parseCreateDirectory(input string) (CreateDirectory, error) {
	pathPattern := regexp.MustCompile(`<path>(.*?)</path>`)
	explanationPattern := regexp.MustCompile(`(?s)<explanation>(.*?)</explanation>`)

	pathMatch := pathPattern.FindStringSubmatch(input)
	if len(pathMatch) < 2 {
		return CreateDirectory{}, fmt.Errorf("path not found in create_directory")
	}
	if strings.TrimSpace(pathMatch[1]) == "" {
		return CreateDirectory{}, fmt.Errorf("empty path in create_directory")
	}

	explanationMatch := explanationPattern.FindStringSubmatch(input)
	explanation := ""
	if len(explanationMatch) >= 2 {
		explanation = strings.TrimSpace(explanationMatch[1])
	}
	if explanation == "" {
		fmt.Printf("Warning: Empty explanation found in %s\n", CreateDirectory{}.Type())
	}

	return CreateDirectory{
		Path:        strings.TrimSpace(pathMatch[1]),
		Explanation: explanation,
	}, nil
}
