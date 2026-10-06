package plugins

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const manifestSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"

type pluginManifest struct {
	name        string
	description string
}

func parseManifest(data []byte) (pluginManifest, []error, error) {
	fields, err := jsonObject(data)
	if err != nil {
		return pluginManifest{}, nil, err
	}
	schema, err := jsonString(fields["$schema"])
	if err != nil || schema != manifestSchema {
		return pluginManifest{}, nil, fmt.Errorf("unsupported or invalid plugin $schema")
	}
	name, err := jsonString(fields["name"])
	if err != nil || !validPluginName(name) {
		return pluginManifest{}, nil, fmt.Errorf("invalid plugin name")
	}
	manifest := pluginManifest{
		name: name,
	}
	var diagnostics []error
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		raw := fields[key]
		switch key {
		case "$schema", "name":
		case "version", "description", "homepage", "repository", "license":
			value, err := jsonString(raw)
			if err != nil {
				return pluginManifest{}, nil, fmt.Errorf(
					"%s: %w",
					key,
					err,
				)
			}
			if key == "description" {
				manifest.description = value
			}
		case "keywords":
			if _, err := jsonStrings(raw); err != nil {
				return pluginManifest{}, nil, fmt.Errorf(
					"keywords: %w",
					err,
				)
			}
		case "author":
			author, err := jsonObject(raw)
			if err != nil {
				return pluginManifest{}, nil, fmt.Errorf(
					"author: %w",
					err,
				)
			}
			for key, raw := range author {
				if key != "name" && key != "email" && key != "url" {
					return pluginManifest{}, nil, fmt.Errorf(
						"unknown author field %q",
						key,
					)
				}
				if _, err := jsonString(raw); err != nil {
					return pluginManifest{}, nil, fmt.Errorf(
						"author.%s: %w",
						key,
						err,
					)
				}
			}
		case "extensions":
			// We implement no extension namespace. Its contents are not
			// validated, even when they violate the machine-readable schema.
			if !bytes.HasPrefix(
				bytes.TrimSpace(raw),
				[]byte("{"),
			) {
				diagnostics = append(
					diagnostics,
					fmt.Errorf("non-object extensions ignored"),
				)
			}
		default:
			diagnostics = append(
				diagnostics,
				fmt.Errorf(
					"unknown manifest field %q ignored",
					key,
				),
			)
		}
	}
	return manifest, diagnostics, nil
}

func validPluginName(name string) bool {
	if len(name) < 1 || len(name) > 64 || strings.Contains(
		name,
		"--",
	) || strings.Contains(
		name,
		"..",
	) {
		return false
	}
	alphanumeric := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' }
	if !alphanumeric(name[0]) || !alphanumeric(name[len(name)-1]) {
		return false
	}
	for _, c := range []byte(name) {
		if !alphanumeric(c) && c != '-' && c != '.' {
			return false
		}
	}
	return true
}
