package auth

import (
	"encoding/base64"
	"reflect"
	"testing"
)

func TestCredentialsIsZero(t *testing.T) {
	t.Parallel()

	if !(Credentials{}).IsZero() {
		t.Fatal("expected zero credentials to report IsZero true")
	}

	creds := Credentials{BasicUsername: "user", BasicPassword: "pass"}
	if creds.IsZero() {
		t.Fatal("expected credentials with basic auth to be non-zero")
	}

	creds = Credentials{Headers: map[string]string{"X-Test": "value"}}
	if creds.IsZero() {
		t.Fatal("expected credentials with extra headers to be non-zero")
	}
}

func TestCredentialsHeaderMapBasic(t *testing.T) {
	t.Parallel()

	creds := Credentials{
		BasicUsername: "demo",
		BasicPassword: "secret",
		Headers:       map[string]string{"X-Trace": "enabled", "Authorization": "ignored"},
	}

	headers := creds.HeaderMap()
	if headers == nil {
		t.Fatal("expected header map")
	}

	token := base64.StdEncoding.EncodeToString([]byte("demo:secret"))
	want := map[string]string{
		"Authorization": "Basic " + token,
		"X-Trace":       "enabled",
	}

	if !reflect.DeepEqual(headers, want) {
		t.Fatalf("unexpected headers: %#v", headers)
	}
}

func TestCredentialsHeaderMapBearerAndAPIKey(t *testing.T) {
	t.Parallel()

	creds := Credentials{
		BearerToken:  "token-123",
		APIKey:       "abc",
		APIKeyHeader: "X-API",
	}

	headers := creds.HeaderMap()
	want := map[string]string{
		"Authorization": "Bearer token-123",
		"X-API":         "abc",
	}

	if !reflect.DeepEqual(headers, want) {
		t.Fatalf("unexpected headers: %#v", headers)
	}
}

func TestCredentialsBasicAuthHelpers(t *testing.T) {
	t.Parallel()

	creds := Credentials{BasicUsername: "demo", BasicPassword: "secret"}
	user, pass, ok := creds.BasicAuth()
	if !ok || user != "demo" || pass != "secret" {
		t.Fatalf("unexpected basic auth values: %q %q %v", user, pass, ok)
	}

	if _, _, ok := (Credentials{}).BasicAuth(); ok {
		t.Fatal("expected empty credentials to report no basic auth")
	}
}

func TestCredentialsBearerHelper(t *testing.T) {
	t.Parallel()

	creds := Credentials{BearerToken: "token"}
	token, ok := creds.Bearer()
	if !ok || token != "token" {
		t.Fatalf("unexpected bearer values: %q %v", token, ok)
	}

	if _, ok := (Credentials{}).Bearer(); ok {
		t.Fatal("expected no bearer token")
	}
}
