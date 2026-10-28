package toolruntime

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/spachava753/cpe/internal/mcpclient"
)

// searchIndex ranks tools with BM25 over each tool's names, description, input
// field names and descriptions, and every description of its server.
type searchIndex struct {
	documents []searchDocument
	average   float64
}

type searchDocument struct {
	tool   *mcpclient.CatalogTool
	terms  map[string]int
	length int
}

var stopWords = map[string]bool{
	"a":    true,
	"an":   true,
	"and":  true,
	"are":  true,
	"as":   true,
	"at":   true,
	"be":   true,
	"by":   true,
	"for":  true,
	"from": true,
	"in":   true,
	"is":   true,
	"it":   true,
	"of":   true,
	"on":   true,
	"or":   true,
	"that": true,
	"the":  true,
	"this": true,
	"to":   true,
	"with": true,
}

var (
	lowerUpper = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	upperRun   = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	nonWord    = regexp.MustCompile(`[^a-z0-9]+`)
)

// stem reduces simple plurals so "issues" matches "issue".
func stem(term string) string {
	switch {
	case len(term) > 4 && strings.HasSuffix(
		term,
		"ies",
	):
		return term[:len(term)-3] + "y"
	case len(term) > 4 && (strings.HasSuffix(
		term,
		"ches",
	) || strings.HasSuffix(
		term,
		"shes",
	) || strings.HasSuffix(
		term,
		"sses",
	) || strings.HasSuffix(
		term,
		"xes",
	) || strings.HasSuffix(
		term,
		"zes",
	)):
		return term[:len(term)-2]
	case len(term) > 3 && strings.HasSuffix(
		term,
		"s",
	) && !strings.HasSuffix(
		term,
		"ss",
	):
		return term[:len(term)-1]
	default:
		return term
	}
}

// tokenize lowercases text, splits camelCase and punctuation, and drops stop words.
func tokenize(text string) []string {
	text = lowerUpper.ReplaceAllString(
		text,
		"$1 $2",
	)
	text = upperRun.ReplaceAllString(
		text,
		"$1 $2",
	)
	var terms []string
	for _, term := range nonWord.Split(
		strings.ToLower(text),
		-1,
	) {
		if term == "" || stopWords[term] {
			continue
		}
		terms = append(
			terms,
			stem(term),
		)
	}
	return terms
}

// schemaText collects property names and descriptions, recursively.
func schemaText(schema any, parts []string) []string {
	object, ok := schema.(map[string]any)
	if !ok {
		return parts
	}
	if description, ok := object["description"].(string); ok {
		parts = append(
			parts,
			description,
		)
	}
	if properties, ok := object["properties"].(map[string]any); ok {
		for name, property := range properties {
			parts = append(
				parts,
				name,
			)
			parts = schemaText(
				property,
				parts,
			)
		}
	}
	parts = schemaText(
		object["items"],
		parts,
	)
	for _, key := range []string{
		"anyOf",
		"oneOf",
		"allOf",
	} {
		if variants, ok := object[key].([]any); ok {
			for _, variant := range variants {
				parts = schemaText(
					variant,
					parts,
				)
			}
		}
	}
	return parts
}

func newSearchIndex(catalog *mcpclient.Catalog) *searchIndex {
	index := &searchIndex{}
	total := 0
	for _, server := range catalog.Servers() {
		for _, tool := range server.Tools {
			parts := []string{
				tool.Name,
				tool.Tool.Name,
				tool.Tool.Title,
				tool.Tool.Description,
				server.Prefix,
				string(server.Spec.ID),
				server.Spec.Description,
				server.Info.Description,
				server.Instructions,
			}
			parts = schemaText(
				normalizeSchema(tool.Tool.InputSchema),
				parts,
			)
			terms := make(map[string]int)
			length := 0
			for _, part := range parts {
				for _, term := range tokenize(part) {
					terms[term]++
					length++
				}
			}
			index.documents = append(
				index.documents,
				searchDocument{
					tool:   tool,
					terms:  terms,
					length: length,
				},
			)
			total += length
		}
	}
	if len(index.documents) != 0 {
		index.average = float64(total) / float64(len(index.documents))
	}
	return index
}

// normalizeSchema accepts schemas decoded as maps or as SDK schema values.
func normalizeSchema(schema any) any {
	if _, ok := schema.(map[string]any); ok || schema == nil {
		return schema
	}
	decoded, err := fromJSONValue(schema)
	if err != nil {
		return nil
	}
	return decoded
}

func (index *searchIndex) search(query string, limit int, server string) []*mcpclient.CatalogTool {
	const k1, b = 1.2, 0.75
	terms := make(map[string]bool)
	for _, term := range tokenize(query) {
		terms[term] = true
	}
	if len(terms) == 0 || index.average == 0 {
		return nil
	}
	count := float64(len(index.documents))
	idf := make(map[string]float64)
	for term := range terms {
		frequency := 0.0
		for _, document := range index.documents {
			if document.terms[term] > 0 {
				frequency++
			}
		}
		idf[term] = math.Log(1 + (count-frequency+0.5)/(frequency+0.5))
	}
	type scored struct {
		tool  *mcpclient.CatalogTool
		score float64
	}
	var matches []scored
	for _, document := range index.documents {
		if server != "" && document.tool.Server.Prefix != server {
			continue
		}
		score := 0.0
		for term := range terms {
			frequency := float64(document.terms[term])
			if frequency == 0 {
				continue
			}
			norm := k1 * (1 - b + b*float64(document.length)/index.average)
			score += idf[term] * frequency * (k1 + 1) / (frequency + norm)
		}
		if score > 0 {
			matches = append(
				matches,
				scored{
					tool:  document.tool,
					score: score,
				},
			)
		}
	}
	sort.SliceStable(
		matches,
		func(i, j int) bool {
			if matches[i].score != matches[j].score {
				return matches[i].score > matches[j].score
			}
			return matches[i].tool.Name < matches[j].tool.Name
		},
	)
	if len(matches) > limit {
		matches = matches[:limit]
	}
	tools := make(
		[]*mcpclient.CatalogTool,
		0,
		len(matches),
	)
	for _, match := range matches {
		tools = append(
			tools,
			match.tool,
		)
	}
	return tools
}
