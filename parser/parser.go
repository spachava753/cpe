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
	Path        string
	Edits       []Edit
	Explanation string
}

type Edit struct {
	Search  string
	Replace string
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

func ParseModifications(input string) ([]Modification, error) {
	var modifications []Modification

	// Define regex patterns for each modification type
	modifyCodePattern := regexp.MustCompile(`(?s)<modify_code>(.*?)</modify_code>`)
	removeFilePattern := regexp.MustCompile(`(?s)<remove_file>(.*?)</remove_file>`)
	createFilePattern := regexp.MustCompile(`(?s)<create_file>(.*?)</create_file>`)

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

	return modifications, nil
}

func parseModifyCode(input string) (ModifyCode, error) {
	pathPattern := regexp.MustCompile(`<path>(.*?)</path>`)
	editPattern := regexp.MustCompile(`(?s)<edit>.*?<search>\s*<!\[CDATA\[(.*?)\]\]>\s*</search>.*?<replace>\s*<!\[CDATA\[(.*?)\]\]>\s*</replace>.*?</edit>`)
	explanationPattern := regexp.MustCompile(`(?s)<explanation>(.*?)</explanation>`)

	pathMatch := pathPattern.FindStringSubmatch(input)
	if len(pathMatch) < 2 {
		return ModifyCode{}, fmt.Errorf("path not found in modify_code")
	}
	if strings.TrimSpace(pathMatch[1]) == "" {
		return ModifyCode{}, fmt.Errorf("empty path in modify_code")
	}

	editMatches := editPattern.FindAllStringSubmatch(input, -1)
	var edits []Edit
	for _, match := range editMatches {
		edits = append(edits, Edit{
			Search:  strings.TrimSpace(match[1]),
			Replace: strings.TrimSpace(match[2]),
		})
	}

	if len(edits) == 0 {
		return ModifyCode{}, fmt.Errorf("no valid edits found in modify_code")
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
		Path:        strings.TrimSpace(pathMatch[1]),
		Edits:       edits,
		Explanation: explanation,
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
	contentPattern := regexp.MustCompile(`(?s)<content>\s*<!\[CDATA\[(.*?)\]\]>\s*</content>`)
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
