package profiler

import (
	"runtime/debug"
	"testing"
)

func TestEnsureGitLabelsUsesExplicitMetadata(t *testing.T) {
	tags := ensureGitLabels(nil, gitMetadataInput{
		repository: "https://github.com/example/project",
		ref:        "deadbeef",
	})

	if got := tags[serviceRepositoryTag]; got != "https://github.com/example/project" {
		t.Fatalf("service_repository tag mismatch: got %q", got)
	}

	if got := tags[serviceGitRefTag]; got != "deadbeef" {
		t.Fatalf("service_git_ref tag mismatch: got %q", got)
	}
}

func TestEnsureGitLabelsUsesBuildInfoFallback(t *testing.T) {
	original := readBuildInfo
	t.Cleanup(func() { readBuildInfo = original })

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Path: "github.com/acme/project/submodule", Version: "", Sum: ""},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "cafebabe"},
				{Key: "vcs.remote", Value: "git@github.com:acme/project.git"},
			},
		}, true
	}

	tags := ensureGitLabels(map[string]string{}, gitMetadataInput{})

	if got := tags[serviceRepositoryTag]; got != "https://github.com/acme/project" {
		t.Fatalf("service_repository tag mismatch: got %q", got)
	}

	if got := tags[serviceGitRefTag]; got != "cafebabe" {
		t.Fatalf("service_git_ref tag mismatch: got %q", got)
	}
}

func TestEnsureGitLabelsDoesNotOverrideExisting(t *testing.T) {
	original := readBuildInfo
	t.Cleanup(func() { readBuildInfo = original })

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Path: "github.com/acme/project"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "cafebabe"},
			},
		}, true
	}

	tags := map[string]string{
		serviceRepositoryTag: "https://github.com/custom/repo",
		serviceGitRefTag:     "custom-ref",
	}

	ensureGitLabels(tags, gitMetadataInput{})

	if got := tags[serviceRepositoryTag]; got != "https://github.com/custom/repo" {
		t.Fatalf("service_repository tag should remain unchanged: got %q", got)
	}

	if got := tags[serviceGitRefTag]; got != "custom-ref" {
		t.Fatalf("service_git_ref tag should remain unchanged: got %q", got)
	}
}
