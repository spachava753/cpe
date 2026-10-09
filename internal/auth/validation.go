package auth

import (
	"errors"
	"unicode"
	"unicode/utf8"
)

func validateAPIKey(key string) error {
	if key == "" || len(key) > 16*1024 || !utf8.ValidString(key) {
		return errors.New("enter a non-empty API key of at most 16 KiB")
	}
	for _, char := range key {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return errors.New("API keys must not contain whitespace or control characters")
		}
	}
	return nil
}
