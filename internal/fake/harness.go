package fake

import (
	"context"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guards.
var (
	_ port.Harness  = (*Harness)(nil)
	_ port.Session  = (*Session)(nil)
	_ port.Harness  = PreparingHarness{}
	_ port.Preparer = PreparingHarness{}
)

// The reasons of sessions the fake harness ends itself.
const (
	// StoppedReason is the Outcome.Reason of a session ended by Stop.
	StoppedReason = "stopped"
	// KilledReason is the Outcome.Reason of a session that ignored Stop and
	// was ended when the stop deadline passed.
	KilledReason = "killed at the stop deadline"
)

// HarnessSettings is the fake harness's config section. It takes model, as
// the claude adapter does; any other key is an error.
type HarnessSettings struct {
	// Model is accepted and ignored.
	Model string `yaml:"model"`
}

// HarnessFactory returns a factory that validates its section into
// HarnessSettings and, when it is valid, returns h itself.
func HarnessFactory(h port.Harness) port.HarnessFactory {
	return func(decode port.Decode) (port.Harness, error) {
		var settings HarnessSettings
		if err := decode(&settings); err != nil {
			return nil, err
		}
		return h, nil
	}
}

// Harness is a scripted harness. Every session it starts runs until the test
// ends it with Session.End, or until it is stopped. Its zero value is not
// usable; use NewHarness.
type Harness struct {
	mu         sync.Mutex
	sessions   []*Session
	next       int           // the index of the session Next returns next
	started    chan struct{} // closed and replaced whenever a session starts
	ignoreStop bool
}

// NewHarness returns a harness with no sessions, whose sessions obey Stop.
func NewHarness() *Harness {
	return &Harness{started: make(chan struct{})}
}

// IgnoreStop makes Stop, on any session and from now on, wait for the stop
// deadline before ending the session, as a session that ignores the
// terminate signal and is killed would.
func (h *Harness) IgnoreStop(ignore bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ignoreStop = ignore
}

func (h *Harness) ignoresStop() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ignoreStop
}

// Sessions returns every session started so far, in start order.
func (h *Harness) Sessions() []*Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*Session(nil), h.sessions...)
}

// Next returns the first session that Next has not returned yet, in start
// order, waiting for one to start until ctx is done.
func (h *Harness) Next(ctx context.Context) (*Session, error) {
	for {
		h.mu.Lock()
		if h.next < len(h.sessions) {
			s := h.sessions[h.next]
			h.next++
			h.mu.Unlock()
			return s, nil
		}
		started := h.started
		h.mu.Unlock()
		select {
		case <-started:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Start implements port.Harness. It records run and returns a running
// session; it never fails.
func (h *Harness) Start(_ context.Context, run port.Run) (port.Session, error) {
	s := &Session{harness: h, run: run, done: make(chan struct{})}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions = append(h.sessions, s)
	close(h.started)
	h.started = make(chan struct{})
	return s, nil
}

// Session is a session of the fake harness.
type Session struct {
	harness *Harness
	run     port.Run

	mu      sync.Mutex
	outcome crew.Outcome
	ended   bool
	stopped bool
	done    chan struct{} // closed when the session ends
}

// Run returns what the session was started with.
func (s *Session) Run() port.Run {
	return s.run
}

// End ends the session with outcome, releasing Wait. It does nothing once
// the session has ended.
func (s *Session) End(outcome crew.Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.outcome, s.ended = outcome, true
	close(s.done)
}

// Wait implements port.Session.
func (s *Session) Wait() crew.Outcome {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outcome
}

// Stop implements port.Session. The session ends as failed with
// StoppedReason; when the harness ignores stop, it ends with KilledReason
// once ctx is done, unless End came first.
func (s *Session) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	if !s.harness.ignoresStop() {
		s.End(crew.Outcome{Reason: StoppedReason})
		return nil
	}
	select {
	case <-s.done:
	case <-ctx.Done():
		s.End(crew.Outcome{Reason: KilledReason})
	}
	return nil
}

// Stopped reports whether Stop was called on the session.
func (s *Session) Stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// PreparingHarness is a Harness that also implements port.Preparer. A plain
// *Harness does not implement it.
type PreparingHarness struct {
	*Harness
	*Preparation
}

// NewPreparingHarness returns a PreparingHarness whose Prepare succeeds until
// told to Fail.
func NewPreparingHarness() PreparingHarness {
	return PreparingHarness{Harness: NewHarness(), Preparation: &Preparation{}}
}
