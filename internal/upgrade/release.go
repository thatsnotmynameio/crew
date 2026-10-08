package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// DefaultAPI is the base URL of GitHub's REST API.
const DefaultAPI = "https://api.github.com"

// APIEnv is the environment variable that replaces DefaultAPI with a
// loopback address, for the acceptance suite's fake GitHub.
const APIEnv = "CREW_UPGRADE_API_URL"

// repository is the repository crew's releases are published in.
const repository = "thatsnotmynameio/crew"

const (
	// apiTimeout bounds each JSON call to GitHub.
	apiTimeout = 30 * time.Second
	// downloadTimeout bounds each asset download.
	downloadTimeout = 10 * time.Minute
	// tokenTimeout bounds gh auth token.
	tokenTimeout = 10 * time.Second
	// maxJSON bounds a JSON reply, a release's or an error's.
	maxJSON = 1 << 20
	// maxArchive bounds a release's archive, and the crew it holds; crew's
	// archives are a few MB.
	maxArchive = 256 << 20
	// maxSums bounds a release's checksums.txt.
	maxSums = 64 << 10
	// maxRedirects is how many redirects a request follows, as Go's
	// client does by default.
	maxRedirects = 10
)

// ErrInterrupted is the error of a call whose context was cancelled, by
// a signal crew upgrade caught: it blames neither GitHub nor the network.
var ErrInterrupted = errors.New("the upgrade was interrupted")

// API is where crew upgrade finds the releases.
type API struct {
	// Base is the API's base URL, with no trailing slash.
	Base string
	// Host is the host gh's login is asked for.
	Host string
	// Override is whether APIEnv set Base.
	Override bool
}

// ResolveAPI returns GitHub's API when override, APIEnv's value, is empty.
// Otherwise override must be an http or https URL of a loopback IP address,
// with at most a port: a name such as localhost, a user, a path, a query or
// a fragment is an EnvError, so the override can neither reach another
// machine nor send gh's login elsewhere.
func ResolveAPI(override string) (API, error) {
	if override == "" {
		return API{Base: DefaultAPI, Host: "github.com"}, nil
	}
	refuse := envErrorf("%s=%q is not an http or https URL of a loopback IP address", APIEnv, override)
	u, err := url.Parse(override)
	if err != nil {
		return API{}, refuse
	}
	ip := net.ParseIP(u.Hostname())
	plain := u.User == nil && u.Opaque == "" && u.Path == "" && u.RawQuery == "" && u.Fragment == "" &&
		!u.ForceQuery
	if (u.Scheme != "http" && u.Scheme != "https") || !plain || ip == nil || !ip.IsLoopback() {
		return API{}, refuse
	}
	return API{Base: override, Host: u.Hostname(), Override: true}, nil
}

// Notice returns the line crew upgrade prints while APIEnv replaces
// GitHub's API, so a stale export never acts silently, or "" when it does
// not.
func (a API) Notice() string {
	if !a.Override {
		return ""
	}
	return "finding crew's releases at " + a.Base + ", set by " + APIEnv
}

// TokenFunc returns the token of the user's gh login for api, or no token
// and the reason there is none.
type TokenFunc func(ctx context.Context, api API) (token, reason string)

// GHToken returns a TokenFunc that asks gh, through run, for the token of
// api's host. Under the override, gh runs without the token variables, so
// only a login stored for that host can answer, and no github.com token
// reaches a local server. Any failure, gh missing included, is no token.
func GHToken(run proc.Runner) TokenFunc {
	return func(ctx context.Context, api API) (string, string) {
		ctx, cancel := context.WithTimeout(ctx, tokenTimeout)
		defer cancel()
		c := proc.Command{Name: "gh", Args: []string{"auth", "token", "--hostname", api.Host}}
		if api.Override {
			// gh answers these before a stored login: the enterprise ones
			// for any host but github.com, the others for github.com.
			c.Unset = []string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"}
		}
		out, err := run(ctx, c)
		switch {
		case errors.Is(err, exec.ErrNotFound):
			return "", "gh is not on PATH"
		case errors.Is(err, context.DeadlineExceeded):
			return "", "gh auth token timed out after " + tokenTimeout.String()
		case err != nil:
			if line, _, _ := strings.Cut(strings.TrimSpace(string(out.Stderr)), "\n"); line != "" {
				return "", clean(line)
			}
			return "", clean(err.Error())
		}
		if token := strings.TrimSpace(string(out.Stdout)); token != "" {
			return token, ""
		}
		return "", "gh auth token printed no token"
	}
}

// Release is a published release of crew.
type Release struct {
	// Tag is its version, vX.Y.Z.
	Tag    string
	Assets []Asset
}

// Asset is a file of a release.
type Asset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Client finds crew's releases and downloads their assets.
type Client struct {
	api   API
	http  *http.Client
	token TokenFunc
	// login is the token of gh's login, looked up on the first call, and
	// reason why there is none when it is "".
	login  string
	reason string
	looked bool
}

// NewClient returns a client of api, sending its requests through hc's
// transport, with the gh login token returns.
func NewClient(api API, hc *http.Client, token TokenFunc) *Client {
	c := &Client{api: api, token: token}
	c.http = &http.Client{Transport: hc.Transport, CheckRedirect: c.checkRedirect}
	return c
}

// Release returns the release of tag, vX.Y.Z, or the latest release when
// tag is "". A release that is a draft, a pre-release, or not the one asked
// is an error, and so is a cancelled ctx, which is ErrInterrupted.
func (c *Client) Release(ctx context.Context, tag string) (Release, error) {
	rel, err := c.release(ctx, tag)
	return rel, interrupted(ctx, err)
}

// Download returns the asset of rel named name, refusing one larger than
// limit bytes. A cancelled ctx is ErrInterrupted.
func (c *Client) Download(ctx context.Context, rel Release, name string, limit int64) ([]byte, error) {
	data, err := c.download(ctx, rel, name, limit)
	return data, interrupted(ctx, err)
}

func (c *Client) release(ctx context.Context, tag string) (Release, error) {
	path := "/repos/" + repository + "/releases/latest"
	if tag != "" {
		path = "/repos/" + repository + "/releases/tags/" + url.PathEscape(tag)
	}
	var reply struct {
		Tag        string  `json:"tag_name"`
		Draft      bool    `json:"draft"`
		Prerelease bool    `json:"prerelease"`
		Assets     []Asset `json:"assets"`
	}
	err := c.getJSON(ctx, path, &reply)
	if se, ok := errors.AsType[*statusError](err); ok && se.code == http.StatusNotFound {
		return Release{}, c.notFound(ctx, tag)
	}
	if err != nil {
		return Release{}, err
	}
	switch {
	case !isTag(reply.Tag) || (tag != "" && reply.Tag != tag):
		return Release{}, fmt.Errorf("GitHub answered release %q for %s", clean(reply.Tag), describe(tag))
	case reply.Draft || reply.Prerelease:
		return Release{}, fmt.Errorf("release %s is not a published release", reply.Tag)
	}
	return Release{Tag: reply.Tag, Assets: reply.Assets}, nil
}

// isTag reports whether tag is a release's tag, vX.Y.Z.
func isTag(tag string) bool {
	m := releaseVersion.FindStringSubmatch(tag)
	return m != nil && tag == "v"+m[1]
}

// describe names the release tag means.
func describe(tag string) string {
	if tag == "" {
		return "the latest release"
	}
	return "release " + tag
}

func (c *Client) download(ctx context.Context, rel Release, name string, limit int64) ([]byte, error) {
	var asset *Asset
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			asset = &rel.Assets[i]
		}
	}
	if asset == nil {
		return nil, fmt.Errorf("release %s has no %s", rel.Tag, name)
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	// The URL is built from the asset's id on the API's base, never taken
	// from the release's JSON, so the token stays on the API's host.
	path := "/repos/" + repository + "/releases/assets/" + strconv.FormatInt(asset.ID, 10)
	resp, err := c.get(ctx, path, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if kind, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); kind == "application/json" {
		return nil, fmt.Errorf("GitHub answered JSON instead of the bytes of %s", name)
	}
	return readCapped(resp.Body, limit, name)
}

// interrupted returns ErrInterrupted, wrapping err, when ctx was cancelled,
// so an interrupt is never reported as GitHub's failure. Otherwise it
// returns err.
func interrupted(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("%w: %w", ErrInterrupted, context.Cause(ctx))
	}
	return err
}

// redirectError is a redirect checkRedirect refused.
type redirectError struct{ reason string }

func (e *redirectError) Error() string { return e.reason }

// checkRedirect keeps a redirect on https when the API is, and sends the
// login's token to no host but the API's own, port included: Go's own
// check compares host names without the port and keeps the token for
// subdomains.
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return &redirectError{reason: fmt.Sprintf("GitHub redirected more than %d times", maxRedirects)}
	}
	if strings.HasPrefix(c.api.Base, "https:") && req.URL.Scheme != "https" {
		return &redirectError{reason: "GitHub redirected to a URL that is not https"}
	}
	if req.URL.Host != via[0].URL.Host {
		req.Header.Del("Authorization")
	}
	return nil
}

// notFound tells a missing release from a repository the login cannot see,
// since GitHub answers 404 to both.
func (c *Client) notFound(ctx context.Context, tag string) error {
	err := c.getJSON(ctx, "/repos/"+repository, nil)
	se, ok := errors.AsType[*statusError](err)
	switch {
	case ok && se.code == http.StatusNotFound && c.login == "":
		return fmt.Errorf("GitHub found no %s: the repository is private and no gh login was found (%s); "+
			"log in with gh auth login", repository, c.reason)
	case ok && se.code == http.StatusNotFound:
		return fmt.Errorf("the gh login cannot see %s", repository)
	case err != nil:
		return err
	case tag == "":
		return fmt.Errorf("%s has no published release", repository)
	default:
		return fmt.Errorf("release %s does not exist", tag)
	}
}

// getJSON reads path's JSON reply into out, or discards it when out is nil.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	resp, err := c.get(ctx, path, "application/vnd.github+json")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := readCapped(resp.Body, maxJSON, "GitHub's reply")
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("GitHub answered an unreadable reply: %w", err)
	}
	return nil
}

// get sends a GET of path that accepts accept and returns a 200 reply. Any
// other status is a *statusError, and no reply is an error that names no
// URL, since a redirect's carries a signed one.
func (c *Client) get(ctx context.Context, path, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api.Base+path, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request: %w", err)
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-Github-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "thatsnotmynameio-crew")
	if !c.looked {
		c.login, c.reason = c.token(ctx, c.api)
		c.looked = true
	}
	if c.login != "" {
		req.Header.Set("Authorization", "Bearer "+c.login)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if re, ok := errors.AsType[*redirectError](err); ok {
			return nil, re
		}
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, replyError(resp, c.login != "")
	}
	return resp, nil
}

// readCapped reads r, refusing more than limit bytes of what, named in
// the error.
func readCapped(r io.Reader, limit int64, what string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub while reading %s: %w", what, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", what, limit)
	}
	return data, nil
}

// statusError is a reply of a status other than 200. It carries the status
// and GitHub's message, never the request.
type statusError struct {
	code    int
	status  string
	message string
	// limited is whether the reply is GitHub's rate limit, and reset when
	// it resets, "" when GitHub did not say.
	limited bool
	reset   string
	// token is whether the request carried the gh login's token.
	token bool
}

func (e *statusError) Error() string {
	switch {
	case e.limited && !e.token:
		return "GitHub answered " + e.status + ", its rate limit; a gh login raises it: gh auth login"
	case e.limited && e.reset != "":
		return "GitHub answered " + e.status + ", its rate limit; it resets at " + e.reset
	case e.limited:
		return "GitHub answered " + e.status + ", its rate limit"
	case e.code == http.StatusUnauthorized && e.token:
		return "GitHub rejected the gh login; run gh auth login"
	case e.message == "":
		return "GitHub answered " + e.status
	}
	return "GitHub answered " + e.status + ": " + e.message
}

// replyError returns the error of resp, a reply of a status other than
// 200 to a request that carried the gh login's token or not, with
// GitHub's message stripped of control characters.
func replyError(resp *http.Response, token bool) error {
	var reply struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, maxJSON)).Decode(&reply)
	limited := resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden &&
		(resp.Header.Get("X-Ratelimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""))
	var reset string
	if at, err := strconv.ParseInt(resp.Header.Get("X-Ratelimit-Reset"), 10, 64); err == nil {
		reset = time.Unix(at, 0).UTC().Format("2006-01-02 15:04:05 UTC")
	}
	return &statusError{
		code: resp.StatusCode, status: clean(resp.Status), message: clean(reply.Message),
		limited: limited, reset: reset, token: token,
	}
}

// clean drops the control characters of s, text from GitHub or gh that
// crew prints, so it cannot move the terminal's cursor or change its
// colours.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
