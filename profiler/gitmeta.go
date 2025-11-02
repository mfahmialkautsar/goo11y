package profiler

import (
	"runtime/debug"
	"strings"
)

const (
	serviceRepositoryTag = "service_repository"
	serviceGitRefTag     = "service_git_ref"
)

var readBuildInfo = debug.ReadBuildInfo

type gitMetadataInput struct {
	repository string
	ref        string
}

func ensureGitLabels(tags map[string]string, meta gitMetadataInput) map[string]string {
	if tags == nil {
		tags = make(map[string]string)
	}

	if _, ok := tags[serviceRepositoryTag]; !ok {
		if repo := detectRepositoryURL(meta); repo != "" {
			tags[serviceRepositoryTag] = repo
		}
	}

	if _, ok := tags[serviceGitRefTag]; !ok {
		if ref := detectGitRef(meta); ref != "" {
			tags[serviceGitRefTag] = ref
		}
	}

	return tags
}

func detectRepositoryURL(meta gitMetadataInput) string {
	if repo := strings.TrimSpace(meta.repository); repo != "" {
		return repo
	}

	if info, ok := readBuildInfo(); ok && info != nil {
		if remote := normalizeGitRemote(getBuildSetting(info.Settings, "vcs.remote")); remote != "" {
			return remote
		}

		if strings.HasPrefix(info.Main.Path, "github.com/") {
			if normalized := normalizeGitHubRepo(strings.TrimPrefix(info.Main.Path, "github.com/")); normalized != "" {
				return normalized
			}
		}
	}

	return ""
}

func detectGitRef(meta gitMetadataInput) string {
	if ref := strings.TrimSpace(meta.ref); ref != "" {
		return ref
	}

	if info, ok := readBuildInfo(); ok && info != nil {
		if revision := strings.TrimSpace(getBuildSetting(info.Settings, "vcs.revision")); revision != "" {
			return revision
		}

		if version := strings.TrimSpace(info.Main.Version); version != "" && version != "(devel)" {
			return version
		}
	}

	return ""
}

func getBuildSetting(settings []debug.BuildSetting, key string) string {
	for _, setting := range settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}

func normalizeGitRemote(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	raw = strings.TrimSuffix(raw, ".git")

	switch {
	case strings.HasPrefix(raw, "https://github.com/"):
		return "https://github.com/" + strings.TrimPrefix(raw, "https://github.com/")
	case strings.HasPrefix(raw, "http://github.com/"):
		return "https://github.com/" + strings.TrimPrefix(raw, "http://github.com/")
	case strings.HasPrefix(raw, "git@github.com:"):
		return "https://github.com/" + strings.TrimPrefix(raw, "git@github.com:")
	case strings.HasPrefix(raw, "github.com/"):
		return "https://github.com/" + strings.TrimPrefix(raw, "github.com/")
	default:
		return ""
	}
}

func normalizeGitHubRepo(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "github.com/")
	raw = strings.TrimSuffix(raw, ".git")
	if raw == "" {
		return ""
	}

	parts := strings.Split(raw, "/")
	if len(parts) < 2 {
		return ""
	}

	owner := parts[0]
	repo := parts[1]

	if owner == "" || repo == "" {
		return ""
	}

	return "https://github.com/" + owner + "/" + repo
}
