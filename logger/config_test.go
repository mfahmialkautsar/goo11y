package logger

import (
	"reflect"
	"testing"

	"github.com/mfahmialkautsar/goo11y/auth"
)

func TestOTLPConfigHeaderMapDefaults(t *testing.T) {
	cfg := OTLPConfig{}

	headers := cfg.headerMap()
	want := map[string][]string{"Content-Type": {"application/json"}}

	if !reflect.DeepEqual(headers, want) {
		t.Fatalf("unexpected headers: %#v", headers)
	}
}

func TestOTLPConfigHeaderMapMergesSources(t *testing.T) {
	cfg := OTLPConfig{
		Headers: map[string]string{
			" x-custom ": " value ",
			"":           "ignored",
			"empty":      " ",
		},
		Credentials: auth.Credentials{
			Headers: map[string]string{
				" Authorization ": "skip",
				"x-extra":         "  extra  ",
			},
			BearerToken: "bear",
		},
	}

	headers := cfg.headerMap()

	expected := map[string][]string{
		"Content-Type":  {"application/json"},
		"x-extra":       {"extra"},
		"Authorization": {"Bearer bear"},
		"x-custom":      {"value"},
	}

	if !reflect.DeepEqual(headers, expected) {
		t.Fatalf("unexpected merged headers: %#v", headers)
	}
}
