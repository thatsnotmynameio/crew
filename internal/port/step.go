package port

import "context"

// stepsKey is the context key of the reporter WithSteps attaches.
type stepsKey struct{}

// WithSteps returns a copy of ctx that carries report, which Step calls with
// each step of the environment checks as it starts: the boot log. Only the
// checks' context carries it, so a call outside the checks, such as a
// Workspace's Create, reports nothing.
func WithSteps(ctx context.Context, report func(step string)) context.Context {
	return context.WithValue(ctx, stepsKey{}, report)
}

// Step reports step, in plain words such as "checking the gh login", to the
// reporter ctx carries, just before the step starts. It does nothing when ctx
// carries none, or once ctx is done, so no step shows after crew was stopped.
func Step(ctx context.Context, step string) {
	report, ok := ctx.Value(stepsKey{}).(func(string))
	if !ok || ctx.Err() != nil {
		return
	}
	report(step)
}
