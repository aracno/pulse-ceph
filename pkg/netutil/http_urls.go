package netutil

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeHTTPBaseURL parses and canonicalizes an HTTP(S) base URL. If the
// input has no scheme, defaultScheme is applied.
func NormalizeHTTPBaseURL(rawURL, defaultScheme string) (*url.URL, error) {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return nil, fmt.Errorf("URL cannot be empty")
	}
	if !strings.Contains(s, "://") {
		if defaultScheme == "" {
			return nil, fmt.Errorf("URL must include http:// or https://")
		}
		s = strings.TrimSuffix(defaultScheme, "://") + "://" + s
	}

	parsed, err := url.ParseRequestURI(s)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("embedded credentials are not allowed")
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("URL fragments are not allowed")
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("URL must have a hostname")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("URL scheme must be http or https")
	}
	parsed.RawQuery = ""
	return parsed, nil
}

// ValidateAbsoluteHTTPURL validates an already absolute HTTP(S) URL.
func ValidateAbsoluteHTTPURL(rawURL string) (*url.URL, error) {
	return NormalizeHTTPBaseURL(rawURL, "")
}
