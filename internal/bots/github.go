package bots

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
	"strings"
	"time"
)

// DefaultAPI is the base URL of GitHub's REST API.
const DefaultAPI = "https://api.github.com"

// apiTimeout bounds each GitHub API call.
const apiTimeout = 30 * time.Second

// maxErrorBody bounds how much of an error reply crew reads for GitHub's
// message.
const maxErrorBody = 1 << 20

// ErrNotInstalled is the error Client.RepoInstallation wraps when the bot
// is not installed on the repository: GitHub answered 404.
var ErrNotInstalled = errors.New("the bot is not installed on the repository")

// ErrKeyRejected is the error a call signed as a bot wraps when GitHub
// answered 401: it rejected the bot's key, as when the app was deleted.
var ErrKeyRejected = errors.New("GitHub rejected the bot's key")

// Client calls GitHub's REST API for the bots. A call signed as a bot
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

// Bot returns the bot called name that the app is. Its bot login is the
// app's slug followed by [bot], as GitHub names app bots.
func (c Conversion) Bot(name string) Bot {
	return Bot{
		Name: name, Owner: c.Owner.Login, OwnerID: c.Owner.ID, AppID: c.AppID, ClientID: c.ClientID,
		Slug: c.Slug, AppName: c.AppName, HTMLURL: c.HTMLURL, BotLogin: botLogin(c.Slug),
		CreatedAt: c.CreatedAt, PrivateKey: c.Key,
	}
}

// Installation is a bot's installation that covers a repository.
type Installation struct {
	// ID is the installation's id.
	ID int64 `json:"id"`
	// RepositorySelection is "selected" when the installation covers the
	// repositories you chose, or "all" for every repository of the
	// owner.
	RepositorySelection string `json:"repository_selection"`
}

// Convert exchanges the code GitHub's redirect carried for the app it
// created. The conversion needs no authentication, and a code converts once.
func (c *Client) Convert(ctx context.Context, code string) (Conversion, error) {
	var conv Conversion
	path := "/app-manifests/" + url.PathEscape(code) + "/conversions"
	if err := c.do(ctx, http.MethodPost, path, auth{}, nil, http.StatusCreated, &conv); err != nil {
		return Conversion{}, fmt.Errorf("convert the app manifest: %w", err)
	}
	return conv, nil
}

// RepoInstallation returns b's installation covering the repository
// owner/name, signed as b. It wraps ErrNotInstalled when b is not installed
// there, and ErrKeyRejected when GitHub rejected b's key.
func (c *Client) RepoInstallation(ctx context.Context, b Bot, owner, name string) (Installation, error) {
	var inst Installation
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/installation"
	err := c.do(ctx, http.MethodGet, path, auth{bot: &b}, nil, http.StatusOK, &inst)
	if se, ok := errors.AsType[*statusError](err); ok && se.code == http.StatusNotFound {
		return Installation{}, fmt.Errorf("%s on %s/%s: %w", b.Name, owner, name, ErrNotInstalled)
	}
	if err != nil {
		return Installation{}, fmt.Errorf("find the installation of %s on %s/%s: %w", b.Name, owner, name, err)
	}
	return inst, nil
}

// Grant is an installation token GitHub minted.
type Grant struct {
	// Token is the token. It never prints.
	Token Token `json:"token"`
	// ExpiresAt is when the token stops working, an hour after it was
	// minted.
	ExpiresAt time.Time `json:"expires_at"`
	// Permissions are the permissions the token grants.
	Permissions map[string]string `json:"permissions"`
}

// AccessToken mints an installation token of b's installation id, limited
// to the repository called name and to the permissions every bot asks
// for. It wraps ErrKeyRejected when GitHub rejected b's key.
func (c *Client) AccessToken(ctx context.Context, b Bot, id int64, name string) (Grant, error) {
	path := "/app/installations/" + strconv.FormatInt(id, 10) + "/access_tokens"
	body := map[string]any{"repositories": []string{name}, "permissions": permissions()}
	var g Grant
	if err := c.do(ctx, http.MethodPost, path, auth{bot: &b}, body, http.StatusCreated, &g); err != nil {
		return Grant{}, fmt.Errorf("mint a token of %s for %s: %w", b.Name, name, err)
	}
	return g, nil
}

// BotUserID returns the user id of the bot of the app slug, asked with
// token, an installation token of that app. It refuses a slug that is not
// lowercase letters, digits and hyphens, and a reply whose id is not a
// positive integer, since both go into a commit trailer.
func (c *Client) BotUserID(ctx context.Context, token Token, slug string) (int64, error) {
	if !validSlug(slug) {
		return 0, fmt.Errorf("the app slug %q holds more than lowercase letters, digits and hyphens", slug)
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(botLogin(slug)), auth{token: token}, nil,
		http.StatusOK, &user); err != nil {
		return 0, fmt.Errorf("find the user of %s[bot]: %w", slug, err)
	}
	if user.ID <= 0 {
		return 0, fmt.Errorf("GitHub gave %s[bot] the user id %d", slug, user.ID)
	}
	return user.ID, nil
}

// validSlug reports whether slug is an app slug crew can put in a file,
// a shell string or a trailer: lowercase letters, digits and hyphens.
func validSlug(slug string) bool {
	return slug != "" && !strings.ContainsFunc(slug, func(r rune) bool { return !nameRune(r) })
}

// botLogin returns the login of the bot of the app slug, such as
// crew-ops[bot].
func botLogin(slug string) string {
	return slug + "[bot]"
}

// auth is how a request authenticates: signed with a fresh app JWT of bot
// when it is not nil, else with token when it is not empty, else not at
// all.
type auth struct {
	bot   *Bot
	token Token
}

// do sends a method request to path with body as JSON, none when nil,
// authenticated as a says. A reply of status want is decoded into out
// unless out is nil; any other status is an error.
func (c *Client) do(ctx context.Context, method, path string, a auth, body any, want int, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := c.request(ctx, method, path, a, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req) //nolint:gosec // G704: the host is the client's base; paths escape what they carry
	if err != nil {
		return fmt.Errorf("GitHub API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		return replyError(resp, a.bot != nil)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("GitHub API: unreadable reply: %w", err)
	}
	return nil
}

// request builds a request with the headers GitHub's API asks for,
// authenticated as a says.
func (c *Client) request(ctx context.Context, method, path string, a auth, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("GitHub API request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	//nolint:gosec // G704: the host is the client's base, and every path escapes what it carries
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
	switch {
	case a.bot != nil:
		jwt, err := AppJWT(a.bot.PrivateKey, a.bot.ClientID, c.now())
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
	case a.token != "":
		req.Header.Set("Authorization", "token "+string(a.token))
	}
	return req, nil
}

// statusError is a reply of a status crew did not expect. It carries the
// status and GitHub's message, never the request.
type statusError struct {
	code    int
	status  string
	message string
	// limited is whether the reply says a rate limit was hit.
	limited bool
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
	err := &statusError{
		code: resp.StatusCode, status: resp.Status, message: reply.Message, limited: rateLimited(resp, reply.Message),
	}
	if signed && resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: %w", ErrKeyRejected, err)
	}
	return err
}

// rateLimited reports whether resp, with GitHub's message, says a rate
// limit was hit: no requests remain, GitHub asks to retry later, or its
// message says so, as for a secondary rate limit.
func rateLimited(resp *http.Response, message string) bool {
	return resp.Header.Get("X-Ratelimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" ||
		strings.Contains(strings.ToLower(message), "rate limit")
}

// transient reports whether err, of a call to GitHub's API, may pass on a
// retry: the call got no reply, or GitHub answered a 5xx, a 429 or a rate
// limit's 403. A key GitHub rejected, any other reply and any error before
// the request was sent are not.
func transient(err error) bool {
	if se, ok := errors.AsType[*statusError](err); ok {
		return se.code >= http.StatusInternalServerError || se.code == http.StatusTooManyRequests ||
			(se.code == http.StatusForbidden && se.limited)
	}
	_, ok := errors.AsType[*url.Error](err)
	return ok
}
