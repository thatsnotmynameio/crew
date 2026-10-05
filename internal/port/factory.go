package port

import "github.com/thatsnotmynameio/crew/internal/crew"

// Decode strictly decodes an adapter's config section into target, which
// must be a non-nil pointer, usually to a struct with `yaml` tags. A key no
// field takes, or a value of the wrong type, is an error naming the key path,
// such as tracker.lables, and its line, ready to show the user as is. A
// key the section leaves out keeps the field's value, so a factory sets its
// defaults before decoding. It is the same type as config.Decode, so the
// config's sections pass to factories directly.
type Decode = func(target any) error

// TrackerFactory builds a tracker adapter from its config section: every key
// under tracker: except name. It validates the section through decode, and
// adds its own checks of the values, before returning; it does not reach the
// network or run tools, which is Preparer's job. states are the rules'
// states, crew.RuleStates: the tracker's states crew owns, and the only
// ones List reports, Move removes and Prepare creates; every other label is
// not crew's.
type TrackerFactory func(decode Decode, states []crew.State) (Tracker, error)

// HarnessFactory builds a harness adapter from its config section: every
// key of an agent's harness except name, model included. It validates the
// section as TrackerFactory does.
type HarnessFactory func(decode Decode) (Harness, error)
