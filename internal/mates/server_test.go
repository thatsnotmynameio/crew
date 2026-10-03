package mates

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

func TestASecondRedirectIsToldTheMateExists(t *testing.T) {
	r := newRun(t, testOwner, true)
	var second response
	r.browser.visit = func(p page) {
		r.browser.confirm(p)
		second = r.browser.created(p, url.Values{"code": {testCode}, "state": {p.state}})
	}
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if second.status != http.StatusOK || !strings.Contains(second.body, "the mate tester exists") {
		t.Errorf("the second /created answered %+v, want the mate exists", second)
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
