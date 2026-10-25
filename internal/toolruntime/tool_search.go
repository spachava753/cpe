package toolruntime

import (
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
)

type toolMatch struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Search only metadata; neither ranking nor inspection invokes remote tools.
func (c toolCatalog) search(query string, limit int, namespace string) []toolMatch {
	terms := searchTerms(query)
	slices.Sort(terms)
	terms = slices.Compact(terms)
	result := []toolMatch{}
	if len(terms) == 0 {
		return result
	}
	type document struct {
		counts map[string]int
		tool   catalogTool
		length float64
		score  float64
	}
	var documents []document
	var totalLength float64
	for _, tool := range c.tools {
		if namespace != "" && tool.Namespace != namespace {
			continue
		}
		words := searchTerms(tool.searchText)
		counts := make(map[string]int)
		for _, word := range words {
			counts[word]++
		}
		length := float64(len(words))
		totalLength += length
		documents = append(
			documents,
			document{
				tool:   tool,
				counts: counts,
				length: length,
			},
		)
	}
	if len(documents) == 0 || totalLength == 0 {
		return result
	}
	averageLength := totalLength / float64(len(documents))
	// Okapi BM25: k1 controls term saturation, b normalizes document length.
	const k1, b = 1.2, 0.75
	for _, term := range terms {
		frequency := 0
		for _, document := range documents {
			if document.counts[term] != 0 {
				frequency++
			}
		}
		idf := math.Log(1 + (float64(len(documents)-frequency)+0.5)/(float64(frequency)+0.5))
		for i := range documents {
			document := &documents[i]
			count := float64(document.counts[term])
			if count != 0 {
				norm := k1 * (1 - b + b*document.length/averageLength)
				document.score += idf * count * (k1 + 1) / (count + norm)
			}
		}
	}
	sort.SliceStable(
		documents,
		func(i, j int) bool { return documents[i].score > documents[j].score },
	)
	for _, document := range documents {
		if document.score <= 0 || len(result) >= limit {
			break
		}
		result = append(
			result,
			toolMatch{
				Name:        document.tool.Name,
				Description: document.tool.Description,
			},
		)
	}
	return result
}

var (
	camelBoundary   = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	acronymBoundary = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
)

func searchTerms(text string) []string {
	text = camelBoundary.ReplaceAllString(
		text,
		"${1} ${2}",
	)
	text = acronymBoundary.ReplaceAllString(
		text,
		"${1} ${2}",
	)
	words := strings.FieldsFunc(
		strings.ToLower(text),
		func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) },
	)
	terms := make(
		[]string,
		0,
		len(words),
	)
	for _, word := range words {
		switch word {
		case "a", "an", "and", "are", "as", "at", "be", "by", "for", "from", "in", "is", "it", "of", "on", "or", "that", "the", "this", "to", "with":
			continue
		}
		switch {
		case len(word) > 4 && strings.HasSuffix(
			word,
			"ies",
		):
			word = word[:len(word)-3] + "y"
		case len(word) > 4 && (strings.HasSuffix(
			word,
			"ches",
		) || strings.HasSuffix(
			word,
			"shes",
		) || strings.HasSuffix(
			word,
			"sses",
		) || strings.HasSuffix(
			word,
			"xes",
		) || strings.HasSuffix(
			word,
			"zes",
		)):
			word = word[:len(word)-2]
		case len(word) > 3 && strings.HasSuffix(
			word,
			"s",
		) && !strings.HasSuffix(
			word,
			"ss",
		):
			word = word[:len(word)-1]
		}
		terms = append(
			terms,
			word,
		)
	}
	return terms
}

func appendSchemaText(parts []string, value any) []string {
	schema, ok := value.(map[string]any)
	if !ok {
		return parts
	}
	if description, ok := schema["description"].(string); ok {
		parts = append(
			parts,
			description,
		)
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		for name, property := range properties {
			parts = appendSchemaText(
				append(
					parts,
					name,
				),
				property,
			)
		}
	}
	for _, key := range []string{
		"$defs",
		"definitions",
	} {
		if definitions, ok := schema[key].(map[string]any); ok {
			for _, definition := range definitions {
				parts = appendSchemaText(
					parts,
					definition,
				)
			}
		}
	}
	parts = appendSchemaText(
		parts,
		schema["items"],
	)
	for _, key := range []string{
		"anyOf",
		"oneOf",
		"allOf",
	} {
		if variants, ok := schema[key].([]any); ok {
			for _, variant := range variants {
				parts = appendSchemaText(
					parts,
					variant,
				)
			}
		}
	}
	return parts
}
