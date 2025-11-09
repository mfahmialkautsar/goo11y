package otlputil

import (
	"fmt"
	"net/url"
	"strings"
)

// Endpoint describes a parsed OTLP endpoint including optional base path and TLS preference.
type Endpoint struct {
	Host     string
	Path     string
	Insecure bool
}

// ParseEndpoint normalizes a user-supplied endpoint, preserving any base path and inferring TLS mode.
//
// Behavior:
//   - Schemes `http` and mark the endpoint as insecure.
//   - Schemes `https` mark the endpoint as secure (Insecure=false).
//   - If no scheme is provided, fallbackInsecure is used to determine TLS and the string is parsed manually.
//   - Any query or fragment components are rejected.
//
// Examples:
//
//	ParseEndpoint("https://collector:4318/custom", true) -> Host "collector:4318", Path "/custom", Insecure false
//	ParseEndpoint("collector:4318", true) -> Host "collector:4318", Path "", Insecure true
//	ParseEndpoint("collector:4318/api", false) -> Host "collector:4318", Path "/api", Insecure false
func ParseEndpoint(raw string, fallbackInsecure bool) (Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Endpoint{}, fmt.Errorf("endpoint is empty")
	}

	if strings.Contains(raw, " ") {
		return Endpoint{}, fmt.Errorf("endpoint must not contain spaces")
	}

	if strings.Contains(raw, "?") || strings.Contains(raw, "#") {
		return Endpoint{}, fmt.Errorf("endpoint must not contain query or fragment")
	}

	if strings.Contains(raw, "://") {
		return parseURLBasedEndpoint(raw, fallbackInsecure)
	}

	return parseHostPathEndpoint(raw, fallbackInsecure)
}

func parseURLBasedEndpoint(raw string, fallbackInsecure bool) (Endpoint, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Endpoint{}, fmt.Errorf("invalid endpoint: %w", err)
	}
	if u.Host == "" {
		return Endpoint{}, fmt.Errorf("endpoint missing host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return Endpoint{}, fmt.Errorf("endpoint must not contain query or fragment")
	}

	path := normalizePath(u.Path)

	return Endpoint{
		Host:     u.Host,
		Path:     path,
		Insecure: schemeIsInsecure(u.Scheme, fallbackInsecure),
	}, nil
}

func parseHostPathEndpoint(raw string, fallbackInsecure bool) (Endpoint, error) {
	var host, path string
	if idx := strings.Index(raw, "/"); idx >= 0 {
		host = strings.TrimSpace(raw[:idx])
		path = raw[idx:]
	} else {
		host = strings.TrimSpace(raw)
	}
	if host == "" {
		return Endpoint{}, fmt.Errorf("endpoint missing host")
	}
	if strings.Contains(host, " ") {
		return Endpoint{}, fmt.Errorf("endpoint host must not contain spaces")
	}

	return Endpoint{
		Host:     strings.TrimRight(host, "/"),
		Path:     normalizePath(path),
		Insecure: fallbackInsecure,
	}, nil
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return ""
	}
	path = strings.TrimRight(path, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func schemeIsInsecure(scheme string, fallback bool) bool {
	switch strings.ToLower(scheme) {
	case "http", "grpc":
		return true
	case "https":
		return false
	default:
		return fallback
	}
}

// HostWithPath returns the host combined with the optional base path, ensuring no trailing slash.
func (e Endpoint) HostWithPath() string {
	return e.Resolve("")
}

// HasPath reports whether the endpoint contained a non-empty base path.
func (e Endpoint) HasPath() bool {
	return e.Path != ""
}

// Resolve returns the endpoint host combined with its base path and the provided suffix.
// When the suffix is already present in the path it is not duplicated. Both the stored
// path and the suffix are normalized to avoid double slashes.
func (e Endpoint) Resolve(suffix string) string {
	host := strings.TrimRight(e.Host, "/")
	combined := combinePath(e.Path, suffix)
	if combined == "" {
		return host
	}
	return host + "/" + combined
}

// PathWithSuffix returns the normalized URL path consisting of the base path and suffix.
// A leading slash is added when the combined path is non-empty.
func (e Endpoint) PathWithSuffix(suffix string) string {
	combined := combinePath(e.Path, suffix)
	if combined == "" {
		return ""
	}
	return "/" + combined
}

func combinePath(base, suffix string) string {
	trimmedBase := strings.Trim(base, "/")
	trimmedSuffix := strings.Trim(suffix, "/")

	switch {
	case trimmedBase == "" && trimmedSuffix == "":
		return ""
	case trimmedBase == "":
		return trimmedSuffix
	case trimmedSuffix == "":
		return trimmedBase
	default:
		if strings.HasSuffix(trimmedBase, trimmedSuffix) {
			return trimmedBase
		}
		return trimmedBase + "/" + trimmedSuffix
	}
}
