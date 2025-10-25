package auth

import (
	"encoding/base64"
	"strings"
)

const defaultAPIKeyHeader = "X-API-Key"

// Credentials captures optional authentication material for HTTP-based telemetry exporters.
type Credentials struct {
	BasicUsername string
	BasicPassword string
	BearerToken   string
	APIKey        string
	APIKeyHeader  string
	Headers       map[string]string
}

// IsZero reports whether the credential set carries no usable data.
func (c Credentials) IsZero() bool {
	if c.BasicUsername != "" || c.BasicPassword != "" {
		return false
	}
	if c.BearerToken != "" || c.APIKey != "" {
		return false
	}
	if c.APIKeyHeader != "" {
		return false
	}
	return len(c.Headers) == 0
}

// HeaderMap materializes the HTTP headers representing the configured credentials.
func (c Credentials) HeaderMap() map[string]string {
	headers := c.extraHeaders()

	if c.APIKey != "" {
		key := c.APIKeyHeader
		if key == "" {
			key = defaultAPIKeyHeader
		}
		headers[key] = c.APIKey
	}

	switch {
	case c.BasicUsername != "" && c.BasicPassword != "":
		token := base64.StdEncoding.EncodeToString([]byte(c.BasicUsername + ":" + c.BasicPassword))
		headers["Authorization"] = "Basic " + token
	case c.BearerToken != "":
		headers["Authorization"] = "Bearer " + c.BearerToken
	}

	if len(headers) == 0 {
		return nil
	}
	return headers
}

// BasicAuth returns the username/password pair if both values are present.
func (c Credentials) BasicAuth() (string, string, bool) {
	if c.BasicUsername == "" || c.BasicPassword == "" {
		return "", "", false
	}
	return c.BasicUsername, c.BasicPassword, true
}

// Bearer returns the configured bearer token if present.
func (c Credentials) Bearer() (string, bool) {
	if c.BearerToken == "" {
		return "", false
	}
	return c.BearerToken, true
}

// extraHeaders clones user-provided headers and strips conflicting Authorization entries.
func (c Credentials) extraHeaders() map[string]string {
	if len(c.Headers) == 0 {
		return make(map[string]string)
	}
	headers := make(map[string]string, len(c.Headers))
	for key, value := range c.Headers {
		if key == "" || value == "" {
			continue
		}
		if strings.EqualFold(key, "authorization") {
			continue
		}
		headers[key] = value
	}
	return headers
}
