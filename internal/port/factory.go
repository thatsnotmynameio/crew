package port

// Decode strictly decodes an adapter's config section into target, which
// must be a non-nil pointer, usually to a struct with `yaml` tags. A key no
// field takes, or a value of the wrong type, is an error naming the key path,
// such as tracker.labels.ready, and its line, ready to show the user as is. A
// key the section leaves out keeps the field's value, so a factory sets its
// defaults before decoding. It is the same type as config.Decode, so the
// config's sections pass to factories directly.
type Decode = func(target any) error

// TrackerFactory builds a tracker adapter from its config section: every key
// under tracker: except name. It validates the section through decode, and
// adds its own checks of the values, before returning; it does not reach the
// network or run tools, which is Preparer's job.
type TrackerFactory func(decode Decode) (Tracker, error)

// HarnessFactory builds a harness adapter from its config section:
// config.model plus the keys of the top-level harness: section. It validates
// the section as TrackerFactory does.
type HarnessFactory func(decode Decode) (Harness, error)
