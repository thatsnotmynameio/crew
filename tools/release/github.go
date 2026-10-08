package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// apiTimeout bounds each GitHub API call.
const apiTimeout = 30 * time.Second

// gitHub reads one repository through GitHub's REST API.
type gitHub struct {
	base  string
	repo  string
	token string
	http  *http.Client
}

// newGitHub returns the repository the Actions environment names: the API
// at GITHUB_API_URL, the repository GITHUB_REPOSITORY and the token
// GITHUB_TOKEN.
func newGitHub(getenv func(string) string) (gitHub, error) {
	gh := gitHub{
		base: strings.TrimSuffix(getenv("GITHUB_API_URL"), "/"), repo: getenv("GITHUB_REPOSITORY"),
		token: getenv("GITHUB_TOKEN"), http: http.DefaultClient,
	}
	for name, value := range map[string]string{
		"GITHUB_API_URL": gh.base, "GITHUB_REPOSITORY": gh.repo, "GITHUB_TOKEN": gh.token,
	} {
		if value == "" {
			return gitHub{}, fmt.Errorf("%s is not set", name)
		}
	}

	return gh, nil
}

// exists reports whether the repository has what path names under it, such
// as git/ref/tags/<tag>: GitHub answered 200, not 404. Any other answer is
// an error naming the URL.
func (gh gitHub) exists(ctx context.Context, path ...string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	escaped := make([]string, len(path))
	for i, segment := range path {
		escaped[i] = url.PathEscape(segment)
	}
	address := gh.base + "/repos/" + gh.repo + "/" + strings.Join(escaped, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return false, fmt.Errorf("GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "thatsnotmynameio-crew-release")
	req.Header.Set("Authorization", "Bearer "+gh.token)
	resp, err := gh.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("GitHub API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("GitHub answered %s for %s", resp.Status, address)
	}
}
