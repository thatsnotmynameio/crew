package mates

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// says fails the test when the run's output does not hold every want.
func (r *flowRun) says(t *testing.T, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("output does not say %q:\n%s", want, r.out)
		}
	}
}

// isEnvError reports whether err is an environment error, exit 2.
func isEnvError(err error) bool {
	_, ok := errors.AsType[*EnvError](err)
	return ok
}

// loopback checks that the run opened only the loopback page, whose form
// posted crew-tester's manifest to GitHub's create page at path with a
// state, and that GitHub's redirect went on to the install page. It
// returns the page.
func (r *flowRun) loopback(t *testing.T, path string) page {
	t.Helper()
	if len(r.browser.pages) != 1 || len(r.browser.opened) != 1 {
		t.Fatalf("opened %v, want only the loopback page", r.browser.opened)
	}
	p := r.browser.pages[0]
	if want := testWeb + path + "?state=" + p.state; p.action != want || p.state == "" {
		t.Errorf("the form posts to %q, want %q with a state", p.action, want)
	}
	if p.manifest.Name != "crew-tester" || p.manifest.RedirectURL != p.base+"/created" {
		t.Errorf("manifest = %+v, want crew-tester redirected to %s/created", p.manifest, p.base)
	}
	if got := r.browser.redirects; len(got) != 1 || got[0].status != http.StatusFound ||
		got[0].location != testInstallURL {
		t.Errorf("/created answered %+v, want a redirect to %s", got, testInstallURL)
	}
	return p
}

func TestCreateMakesAndInstallsANewMateForAnOrganization(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.installedAt = 3
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	p := r.loopback(t, "/organizations/thatsnotmynameio/settings/apps/new")
	m, err := r.store.Load(testOwner, "tester")
	if err != nil || m.BotLogin != "crew-tester[bot]" || m.AppID != 7 {
		t.Errorf("saved mate = %+v, %v; want crew-tester[bot]", m, err)
	}
	if conversions, lookups, tokens := r.api.counts(); conversions != 1 || lookups != 3 || tokens != 1 {
		t.Errorf("GitHub got %d conversions, %d lookups, %d token calls; want 1, 3, 1", conversions, lookups, tokens)
	}
	r.says(t, p.base+"/", testInstallURL, `"Only select repositories"`)
	if got, want := r.lastLine(), "crew: mate tester is ready on thatsnotmynameio/crew as crew-tester[bot]"; got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
	closed(t, p.base)
}

func TestCreateForAUserRepositoryPostsToTheUsersPage(t *testing.T) {
	r := newRun(t, "matheus", false)
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	r.loopback(t, "/settings/apps/new")
	if _, err := r.store.Load("matheus", "tester"); err != nil {
		t.Errorf("the mate is not saved for its user: %v", err)
	}
}

func TestCreateRecordsTheAppNameGitHubCreated(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.slug = "thatsnotmyname-crew-dev" // AE2: the boss renamed crew-dev
	if err := r.create(t, "dev"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := r.browser.pages[0].manifest.Name; got != "crew-dev" {
		t.Errorf("suggested %q, want crew-dev", got)
	}
	m, err := r.store.Load(testOwner, "dev")
	if err != nil || m.Slug != "thatsnotmyname-crew-dev" || m.BotLogin != "thatsnotmyname-crew-dev[bot]" {
		t.Errorf("saved mate = %+v, %v; want the renamed app", m, err)
	}
	if got := r.browser.redirects[0].location; got != testWeb+"/apps/thatsnotmyname-crew-dev/installations/new" {
		t.Errorf("redirected to %q, want the renamed app's install page", got)
	}
	if !strings.HasSuffix(r.lastLine(), "as thatsnotmyname-crew-dev[bot]") {
		t.Errorf("last line = %q, want the renamed bot", r.lastLine())
	}
}

func TestCreateKeepsTheMateWhenTheInstallationNeverComes(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.installedAt = 0
	r.flow.InstallTimeout = 30 * time.Millisecond
	err := r.create(t, "tester")
	if err == nil || isEnvError(err) || !strings.Contains(err.Error(), testInstallURL) {
		t.Fatalf("Create = %v, want a failure carrying the install URL", err)
	}
	r.says(t, testInstallURL)
	if _, err := r.store.Load(testOwner, "tester"); err != nil {
		t.Errorf("the mate was not kept: %v", err)
	}
}

func TestCreateStoppedWhileWaitingForTheInstallationKeepsTheMate(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.installedAt = 0
	ctx, cancel := context.WithCancel(context.Background())
	r.ctx = ctx
	time.AfterFunc(20*time.Millisecond, cancel)
	err := r.create(t, "tester")
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), testInstallURL) {
		t.Fatalf("Create = %v, want the stop and the install URL", err)
	}
	if _, err := r.store.Load(testOwner, "tester"); err != nil {
		t.Errorf("the mate was not kept: %v", err)
	}
}

func TestCreateInstallsASavedMateWithoutCreatingAnApp(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.installedAt = 2
	if err := r.store.Save(testMate(testOwner)); err != nil {
		t.Fatal(err)
	}
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(r.browser.opened) != 1 || r.browser.opened[0] != testInstallURL {
		t.Errorf("opened %v, want only the install page", r.browser.opened)
	}
	if conversions, _, tokens := r.api.counts(); conversions != 0 || tokens != 1 {
		t.Errorf("GitHub got %d conversions and %d token calls, want 0 and 1", conversions, tokens)
	}
	if got := r.lastLine(); got != "crew: mate tester is ready on thatsnotmynameio/crew as crew-tester[bot]" {
		t.Errorf("last line = %q", got)
	}
}

func TestCreateMakesANewAppWhenOnlyAnotherOwnerHasTheMate(t *testing.T) {
	r := newRun(t, "matheus", false)
	if err := r.store.Save(testMate(testOwner)); err != nil {
		t.Fatal(err)
	}
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if conversions, _, _ := r.api.counts(); len(r.browser.pages) != 1 || conversions != 1 {
		t.Errorf("loaded %d pages and converted %d apps, want one new app", len(r.browser.pages), conversions)
	}
	if _, err := r.store.Load("matheus", "tester"); err != nil {
		t.Errorf("no mate for matheus: %v", err)
	}
}

func TestCreateSavesNothingWhenGitHubNeverRedirects(t *testing.T) {
	for name, stop := range map[string]func(r *flowRun){
		"timeout": func(r *flowRun) { r.flow.CreateTimeout = 30 * time.Millisecond },
		"stop signal": func(r *flowRun) {
			ctx, cancel := context.WithCancel(context.Background())
			r.ctx = ctx
			r.browser.visit = func(page) { cancel() }
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRun(t, testOwner, true)
			r.browser.visit = func(page) {} // the boss closes GitHub's page
			stop(r)
			err := r.create(t, "tester")
			if err == nil || isEnvError(err) {
				t.Fatalf("Create = %v, want a runtime failure", err)
			}
			if _, err := r.store.Load(testOwner, "tester"); !errors.Is(err, ErrNoMate) {
				t.Errorf("Load = %v, want nothing saved", err)
			}
			closed(t, r.browser.pages[0].base)
		})
	}
}

func TestCreateFailsWhenTheConversionFails(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.convStatus = http.StatusUnprocessableEntity
	err := r.create(t, "tester")
	if err == nil || isEnvError(err) || !strings.Contains(err.Error(), "Validation Failed") {
		t.Fatalf("Create = %v, want the conversion's failure", err)
	}
	// GitHub created the app before its redirect, so its key is lost.
	advice := "GitHub may have created the app crew-tester for " + testOwner + " already: delete it at " +
		testWeb + "/organizations/" + testOwner + "/settings/apps before running crew mates create tester again"
	if !strings.Contains(err.Error(), advice) {
		t.Errorf("Create = %v, want the advice %q", err, advice)
	}
	if got := r.browser.redirects[0]; got.status == http.StatusFound {
		t.Errorf("/created answered %+v, want no redirect to the install page", got)
	}
	if _, err := r.store.Load(testOwner, "tester"); !errors.Is(err, ErrNoMate) {
		t.Errorf("Load = %v, want nothing saved", err)
	}
}

func TestAFailedConversionOnAUserRepositoryPointsAtTheUsersApps(t *testing.T) {
	r := newRun(t, "matheus", false)
	r.api.convStatus = http.StatusBadGateway
	err := r.create(t, "tester")
	if want := "delete it at " + testWeb + "/settings/apps before"; err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Create = %v, want %q", err, want)
	}
}

func TestCreateConfirmsAnInstalledMateWithoutOpeningAnything(t *testing.T) {
	r := newRun(t, testOwner, true)
	if err := r.store.Save(testMate(testOwner)); err != nil {
		t.Fatal(err)
	}
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(r.browser.opened) != 0 || strings.Contains(r.out.String(), testInstallURL) {
		t.Errorf("opened %v and printed:\n%s\nwant no install page", r.browser.opened, r.out)
	}
	if _, lookups, tokens := r.api.counts(); lookups != 1 || tokens != 1 {
		t.Errorf("GitHub got %d lookups and %d token calls, want 1 each", lookups, tokens)
	}
}

func TestCreateGoesOnWhenTheBrowserCannotOpen(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.browser.openErr = errors.New("xdg-open: not found")
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	r.says(t, "crew: could not open the browser (xdg-open: not found); open "+r.browser.pages[0].base+"/ yourself")
}

func TestCreateForAnotherAccountKeepsTheMateThereAndFails(t *testing.T) {
	r := newRun(t, testOwner, false)
	r.api.convOwner = "matheus" // the boss's browser was signed in as matheus
	err := r.create(t, "tester")
	if err == nil || isEnvError(err) {
		t.Fatalf("Create = %v, want a runtime failure", err)
	}
	for _, want := range []string{"matheus", testOwner, r.store.Path("matheus", "tester")} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Create = %v, want it to name %q", err, want)
		}
	}
	if _, err := r.store.Load("matheus", "tester"); err != nil {
		t.Errorf("the mate was not kept for matheus: %v", err)
	}
	if _, err := r.store.Load(testOwner, "tester"); !errors.Is(err, ErrNoMate) {
		t.Errorf("Load for the repository's owner = %v, want none", err)
	}
}

func TestCreateStopsAtOnceWhenGitHubRejectsTheKey(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.lookupStatus = http.StatusUnauthorized
	if err := r.store.Save(testMate(testOwner)); err != nil {
		t.Fatal(err)
	}
	err := r.create(t, "tester")
	if err == nil || isEnvError(err) {
		t.Fatalf("Create = %v, want a runtime failure", err)
	}
	for _, want := range []string{r.store.Path(testOwner, "tester"), "deleted on GitHub", "crew mates create tester"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Create = %v, want it to say %q", err, want)
		}
	}
	if strings.Contains(err.Error()+r.out.String(), "/installations/new") {
		t.Errorf("Create = %v, printed:\n%s\nwant no install URL", err, r.out)
	}
	if _, lookups, _ := r.api.counts(); lookups != 1 {
		t.Errorf("GitHub got %d lookups, want 1", lookups)
	}
}

func TestCreateSignsEveryPollWithAFreshJWT(t *testing.T) {
	r := newRun(t, testOwner, true)
	if err := r.store.Save(testMate(testOwner)); err != nil {
		t.Fatal(err)
	}
	// Each JWT is minted four minutes after the last, so the fifth lookup
	// comes past one JWT's lifetime, when a reused JWT would be expired.
	var mu sync.Mutex
	clock := testNow
	r.api.clock = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	r.client.now = func() time.Time { mu.Lock(); defer mu.Unlock(); clock = clock.Add(4 * time.Minute); return clock }
	r.api.installedAt = 5
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, lookups, tokens := r.api.counts(); lookups != 5 || tokens != 1 {
		t.Errorf("GitHub got %d lookups and %d token calls, want 5 and 1", lookups, tokens)
	}
}

func TestCreateWarnsWhenTheMateCanActOnEveryRepository(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.installedAt, r.api.selection = 2, "all"
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	r.says(t, `crew: choose "Only select repositories" and select crew`,
		"crew: warning: crew-tester is installed on every repository of thatsnotmynameio")
	if !strings.HasPrefix(r.lastLine(), "crew: mate tester is ready") {
		t.Errorf("last line = %q, want the success", r.lastLine())
	}
}

func TestCreateFailsWhenTheMatesFileAppearedMeanwhile(t *testing.T) {
	r := newRun(t, testOwner, true)
	other := testMate(testOwner)
	other.AppID = 1234
	r.browser.visit = func(p page) {
		if err := r.store.Save(other); err != nil { // another run saved it first
			t.Fatal(err)
		}
		r.browser.confirm(p)
	}
	path := r.store.Path(testOwner, "tester")
	err := r.create(t, "tester")
	if err == nil || isEnvError(err) || !strings.Contains(err.Error(), "https://github.test/apps/crew-tester") ||
		!strings.Contains(err.Error(), "delete") {
		t.Fatalf("Create = %v, want a failure naming the created app to delete", err)
	}
	if m, err := r.store.Load(testOwner, "tester"); err != nil || m.AppID != 1234 {
		t.Errorf("the existing mate's file changed: %+v, %v", m, err)
	}
	if got := r.browser.redirects[0]; got.status == http.StatusFound {
		t.Errorf("/created answered %+v, want no redirect to the install page", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the existing file is gone: %v", err)
	}
}

func TestCreateFailsWhenNoTokenIsMinted(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.tokenStatus = http.StatusUnprocessableEntity
	err := r.create(t, "tester")
	if err == nil || isEnvError(err) || !strings.Contains(err.Error(), testInstallURL) {
		t.Fatalf("Create = %v, want a failure carrying the install URL", err)
	}
	if _, err := r.store.Load(testOwner, "tester"); err != nil {
		t.Errorf("the mate was not kept: %v", err)
	}
}

// failingGh makes the run's gh fail as it does without a GitHub remote.
func failingGh(_ *testing.T, r *flowRun) {
	r.flow.Gh, r.ghCalls = scriptedGh("", errors.New("gh: exit status 1: no git remotes found"))
}

// brokenMate gives the repository's owner a mate tester whose file is not
// JSON.
func brokenMate(t *testing.T, r *flowRun) {
	t.Helper()
	path := r.store.Path(testOwner, "tester")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCreateChecksTheEnvironmentBeforeOpeningTheBrowser(t *testing.T) {
	for name, tc := range map[string]struct {
		setup  func(t *testing.T, r *flowRun)
		mate   string
		ranGh  bool
		saying string
	}{
		"invalid name":          {mate: "Tester", saying: "lowercase", setup: func(*testing.T, *flowRun) {}},
		"gh fails":              {mate: "tester", saying: "gh auth login", setup: failingGh, ranGh: true},
		"unreadable saved mate": {mate: "tester", saying: "not valid JSON", setup: brokenMate, ranGh: true},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRun(t, testOwner, true)
			tc.setup(t, r)
			err := r.create(t, tc.mate)
			if !isEnvError(err) || !strings.Contains(err.Error(), tc.saying) {
				t.Fatalf("Create = %v, want an environment error saying %q", err, tc.saying)
			}
			if len(r.browser.opened) != 0 || (len(*r.ghCalls) > 0) != tc.ranGh {
				t.Errorf("opened %v and ran gh %d times", r.browser.opened, len(*r.ghCalls))
			}
		})
	}
}

func TestCreateNeverPrintsTheKey(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.installedAt, r.api.selection = 2, "all"
	if err := r.create(t, "tester"); err != nil { // create checks every run's output
		t.Fatalf("Create: %v", err)
	}
	data, err := os.ReadFile(r.store.Path(testOwner, "tester"))
	if err != nil || !strings.Contains(string(data), "PRIVATE KEY") {
		t.Errorf("the mate's file does not hold the key: %v", err)
	}
}
