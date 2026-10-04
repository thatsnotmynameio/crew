package mates

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"unicode"
)

// renewals is the outcome of each mate's last renewal: the warning of every
// mate whose last renewal failed. Its zero value holds none.
type renewals struct {
	mu     sync.Mutex
	failed map[string]string
}

// record records the last renewal of the mate called name: warning when it
// failed, "" when it succeeded.
func (r *renewals) record(name, warning string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if warning == "" {
		delete(r.failed, name)
		return
	}
	if r.failed == nil {
		r.failed = map[string]string{}
	}
	r.failed[name] = warning
}

// Failing returns, by mate name, the warning of each mate whose last
// renewal failed; a mate whose last renewal succeeded is absent. The map is
// a copy, and Failing is safe to call while the renewal loop runs.
func (a *Acting) Failing() map[string]string {
	a.renewals.mu.Lock()
	defer a.renewals.mu.Unlock()
	failing := make(map[string]string, len(a.renewals.failed))
	maps.Copy(failing, a.renewals.failed)
	return failing
}

// renewWarning is the warning of mate m, whose token renewal failed because
// of err, or "" when err is nil.
func renewWarning(m Mate, store *Store, err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrKeyRejected):
		return tokenWarning(m, store, err)
	}
	return fmt.Sprintf("mate %s could not renew its token: %s; its sessions and checks fail once the "+
		"current token expires, and crew tries again every minute", m.Name, oneLine(err.Error()))
}

// oneLine returns text cut at its first line break, without control
// characters, so outside text such as an API client's error can go into a
// warning that --plain prints as it is.
func oneLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, line)
}
