package crew

// Optional is a value that may be absent, such as a session's cost that a
// harness may or may not report. Some builds one with a value; the zero
// Optional has none. Two optionals of a comparable T are equal when both
// have no value or both have equal values.
type Optional[T any] struct {
	value T
	ok    bool
}

// Some returns an Optional holding v.
func Some[T any](v T) Optional[T] {
	return Optional[T]{value: v, ok: true}
}

// Get returns the value and true, or the zero T and false when there is
// none.
func (o Optional[T]) Get() (T, bool) {
	return o.value, o.ok
}
