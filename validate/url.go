package validate

import (
	"fmt"
	"net/url"
)

// ValidateURL checks if the given string is a valid HTTP or HTTPS URL.
func ValidateURL(urlString string) (*url.URL, error) {
	parsedURL, err := url.Parse(urlString)
	if err != nil {
		return nil, err
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("invalid URL scheme: %s", parsedURL.Scheme)
	}
	return parsedURL, nil
}
