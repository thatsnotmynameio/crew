package mates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultAPI is the base URL of GitHub's REST API.
const DefaultAPI = "https://api.github.com"

// apiTimeout bounds each GitHub API call.
const apiTimeout = 30 * time.Second

// maxErrorBody bounds how much of an error reply crew reads for GitHub's
// message.
const maxErrorBody = 1 << 20

// ErrNotInstalled is the error Client.RepoInstallation wraps when the mate
// is not installed on the repository: GitHub answered 404.
var ErrNotInstalled = errors.New("the mate is not installed on the repository")

// ErrKeyRejected is the error a call signed as a mate wraps when GitHub
// answered 401: it rejected the mate's key, as when the app was deleted.
var ErrKeyRejected = errors.New("GitHub rejected the mate's key")

// Client calls GitHub's REST API for the mates. A call signed as a mate
// mints a fresh app JWT for its request, since one lives under 10 minutes
// and a wait for the installation can last longer.
type Client struct {
	base    string
	http    *http.Client
	now     func() time.Time
	timeout time.Duration
}

// NewClient returns a client of the API at base, such as DefaultAPI,
// sending its requests through hc.
func NewClient(base string, hc *http.Client) *Client {
	return &Client{base: base, http: hc, now: time.Now, timeout: apiTimeout}
}

// Conversion is the app GitHub created from a manifest, as its conversion
// returned it.
type Conversion struct {
	AppID     int64      `json:"id"`
	Slug      string     `json:"slug"`
	ClientID  string     `json:"client_id"`
	AppName   string     `json:"name"`
	HTMLURL   string     `json:"html_url"`
	CreatedAt time.Time  `json:"created_at"`
	Key       PrivateKey `json:"pem"`
	Owner     struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	} `json:"owner"`
}

// Mate returns the mate called name that the app is. Its bot login is the
// app's slug followed by [bot], as GitHub names app bots.
func (c Conversion) Mate(name string) Mate {
	return Mate{
		Name: name, Owner: c.Owner.Login, OwnerID: c.Owner.ID, AppID: c.AppID, ClientID: c.ClientID,
		Slug: c.Slug, AppName: c.AppName, HTMLURL: c.HTMLURL, BotLogin: c.Slug + "[bot]",
		CreatedAt: c.CreatedAt, PrivateKey: c.Key,
	}
}

// Installation is a mate's installation that covers a repository.
type Installation struct {
	// ID is the installation's id.
	ID int64 `json:"id"`
	// RepositorySelection is "selected" when the installation covers the
	// repositories the boss chose, or "all" for every repository of the
	// owner.
	RepositorySelection string `json:"repository_selection"`
}

// Convert exchanges the code GitHub's redirect carried for the app it
// created. The conversion needs no authentication, and a code converts once.
func (c *Client) Convert(ctx context.Context, code string) (Conversion, error) {
	var conv Conversion
	path := "/app-manifests/" + url.PathEscape(code) + "/conversions"
	if err := c.do(ctx, http.MethodPost, path, nil, nil, http.StatusCreated, &conv); err != nil {
		return Conversion{}, fmt.Errorf("convert the app manifest: %w", err)
	}
	return conv, nil
}

// RepoInstallation returns m's installation covering the repository
// owner/name, signed as m. It wraps ErrNotInstalled when m is not installed
// there, and ErrKeyRejected when GitHub rejected m's key.
func (c *Client) RepoInstallation(ctx context.Context, m Mate, owner, name string) (Installation, error) {
	var inst Installation
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/installation"
	err := c.do(ctx, http.MethodGet, path, &m, nil, http.StatusOK, &inst)
	if se, ok := errors.AsType[*statusError](err); ok && se.code == http.StatusNotFound {
		return Installation{}, fmt.Errorf("%s on %s/%s: %w", m.Name, owner, name, ErrNotInstalled)
	}
	if err != nil {
		return Installation{}, fmt.Errorf("find the installation of %s on %s/%s: %w", m.Name, owner, name, err)
	}
	return inst, nil
}

// AccessToken mints an installation token of m's installation id, limited
// to the repository called name, and discards it: a minted token proves m
// can act on the repository. It wraps ErrKeyRejected when GitHub rejected
// m's key.
func (c *Client) AccessToken(ctx context.Context, m Mate, id int64, name string) error {
	path := "/app/installations/" + strconv.FormatInt(id, 10) + "/access_tokens"
	body := map[string][]string{"repositories": {name}}
	if err := c.do(ctx, http.MethodPost, path, &m, body, http.StatusCreated, nil); err != nil {
		return fmt.Errorf("mint a token of %s for %s: %w", m.Name, name, err)
	}
	return nil
}

// do sends a method request to path with body as JSON, none when nil,
// signed as signer when it is not nil. A reply of status want is decoded
// into out unless out is nil; any other status is an error.
func (c *Client) do(ctx context.Context, method, path string, signer *Mate, body any, want int, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := c.request(ctx, method, path, signer, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		return replyError(resp, signer != nil)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("GitHub API: unreadable reply: %w", err)
	}
	return nil
}

// request builds a request with the headers GitHub's API asks for, signed
// with a fresh app JWT of signer when it is not nil.
func (c *Client) request(ctx context.Context, method, path string, signer *Mate, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("GitHub API request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "thatsnotmynameio-crew")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if signer != nil {
		jwt, err := AppJWT(signer.PrivateKey, signer.ClientID, c.now())
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	return req, nil
}

// statusError is a reply of a status crew did not expect. It carries the
// status and GitHub's message, never the request.
type statusError struct {
	code    int
	status  string
	message string
}

func (e *statusError) Error() string {
	if e.message == "" {
		return "GitHub answered " + e.status
	}
	return "GitHub answered " + e.status + ": " + e.message
}

// replyError returns the error of resp, a reply of a status crew did not
// expect, wrapping ErrKeyRejected when a signed request got 401.
func replyError(resp *http.Response, signed bool) error {
	var reply struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, maxErrorBody)).Decode(&reply)
	err := &statusError{code: resp.StatusCode, status: resp.Status, message: reply.Message}
	if signed && resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: %w", ErrKeyRejected, err)
	}
	return err
}
