package bots

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestARedirectWithTheWrongStateIsRefusedAndTheWaitGoesOn(t *testing.T) {
	r := newRun(t, testOwner, true)
	var refused []response
	r.browser.visit = func(p page) {
		refused = append(refused,
			r.browser.created(p, url.Values{"code": {testCode}, "state": {"forged"}}),
			r.browser.created(p, url.Values{"code": {testCode}}))
		r.browser.confirm(p)
	}
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, got := range refused {
		if got.status != http.StatusBadRequest {
			t.Errorf("/created with a wrong state answered %d, want 400", got.status)
		}
	}
	if conversions, _, _ := r.api.counts(); conversions != 1 {
		t.Errorf("GitHub got %d conversions, want only the right state's", conversions)
	}
}

func TestASecondRedirectIsToldTheBotExists(t *testing.T) {
	r := newRun(t, testOwner, true)
	var second response
	r.browser.visit = func(p page) {
		r.browser.confirm(p)
		second = r.browser.created(p, url.Values{"code": {testCode}, "state": {p.state}})
	}
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if second.status != http.StatusOK || !strings.Contains(second.body, "the bot tester exists") {
		t.Errorf("the second /created answered %+v, want the bot exists", second)
	}
	if conversions, _, _ := r.api.counts(); conversions != 1 {
		t.Errorf("GitHub got %d conversions, want 1", conversions)
	}
}

func TestTheLoopbackServerAnswersNothingElse(t *testing.T) {
	r := newRun(t, testOwner, true)
	var others []response
	r.browser.visit = func(p page) {
		others = append(others, r.browser.get(p.base+"/favicon.ico"), r.browser.get(p.base+"/created/again"))
		r.browser.confirm(p)
	}
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, got := range others {
		if got.status != http.StatusNotFound {
			t.Errorf("another path answered %d, want 404", got.status)
		}
	}
}

func TestTheLoopbackServerRefusesAnotherHost(t *testing.T) {
	r := newRun(t, testOwner, true)
	var refused []response
	var local response
	r.browser.visit = func(p page) {
		port := p.base[strings.LastIndex(p.base, ":")+1:]
		query := url.Values{"code": {testCode}, "state": {p.state}}.Encode()
		for _, u := range []string{p.base + "/", p.base + "/created?" + query} {
			got, err := r.browser.do(u, "evil.example:"+port) // a DNS rebinding page
			if err != nil {
				t.Fatal(err)
			}
			refused = append(refused, got)
		}
		var err error
		if local, err = r.browser.do(p.base+"/", "localhost:"+port); err != nil { // a browser over an SSH forward
			t.Fatal(err)
		}
		r.browser.confirm(p)
	}
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, got := range refused {
		if got.status != http.StatusMisdirectedRequest || strings.Contains(got.body, r.browser.pages[0].state) {
			t.Errorf("a request for another host answered %+v, want 421 without the state", got)
		}
	}
	if local.status != http.StatusOK {
		t.Errorf("a request for localhost answered %d, want 200", local.status)
	}
	if conversions, _, _ := r.api.counts(); conversions != 1 {
		t.Errorf("GitHub got %d conversions, want only the right host's", conversions)
	}
}

func TestARedirectAfterAFailedExchangeIsAConflict(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.convStatus = http.StatusUnprocessableEntity
	var second response
	r.browser.visit = func(p page) {
		r.browser.confirm(p)
		second = r.browser.created(p, url.Values{"code": {testCode}, "state": {p.state}})
	}
	if err := r.create(t, "tester"); err == nil {
		t.Fatal("Create succeeded, want the conversion's failure")
	}
	if got := r.browser.redirects[0]; got.status != http.StatusInternalServerError {
		t.Errorf("the first /created answered %+v, want 500", got)
	}
	if second.status != http.StatusConflict || !strings.Contains(second.body, "already handled") {
		t.Errorf("the second /created answered %+v, want 409", second)
	}
	if conversions, _, _ := r.api.counts(); conversions != 1 {
		t.Errorf("GitHub got %d conversions, want 1", conversions)
	}
}
