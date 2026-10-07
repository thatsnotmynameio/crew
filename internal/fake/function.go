package fake

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guard.
var _ port.Function = (*Function)(nil)

// FunctionSettings are the fake function's parameters: a text, a number and
// a boolean; any other parameter is an error.
type FunctionSettings struct {
	// Title is a text parameter.
	Title string `yaml:"title"`
	// Count is a number parameter.
	Count int `yaml:"count"`
	// Draft is a boolean parameter.
	Draft bool `yaml:"draft"`
}

// FunctionResult is how one call of a fake function ends.
type FunctionResult struct {
	// Verdict is the verdict the call returns; empty returns crew.Passed.
	Verdict crew.Verdict
	// Err, when set, is the error the call returns instead of a verdict.
	Err error
	// Print is written to the call's log first.
	Print string
	// Block makes the call run until its context ends, after printing, and
	// return an error wrapping the context's.
	Block bool
}

// FunctionRecord is a call a fake function received.
type FunctionRecord struct {
	// Call is the call as made.
	Call port.FunctionCall
	// Settings are the call's parameters, decoded.
	Settings FunctionSettings
}

// Function is a scripted function: each call ends as the next result
// Script queued, and a call with none left returns crew.Passed. It records
// every call and every build its FunctionDefinition made. It is safe for
// concurrent use. Its zero value is not usable; use NewFunction.
type Function struct {
	mu      sync.Mutex
	results []FunctionResult
	calls   []FunctionRecord
	builds  []FunctionSettings
	refusal *port.RefusedParameterError
}

// NewFunction returns a function whose calls all return crew.Passed until
// scripted.
func NewFunction() *Function {
	return &Function{}
}

// Script queues results, one for each of the next calls, in order.
func (f *Function) Script(results ...FunctionResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results = append(f.results, results...)
}

// Refuse makes FunctionDefinition's factory refuse every later build with
// refusal.
func (f *Function) Refuse(refusal port.RefusedParameterError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refusal = &refusal
}

// Calls returns the calls received so far, in the order they started.
func (f *Function) Calls() []FunctionRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Builds returns the parameters each build of FunctionDefinition's factory
// decoded, in order, refused builds included.
func (f *Function) Builds() []FunctionSettings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.builds)
}

// Run implements port.Function. It decodes the call's parameters into
// FunctionSettings and returns the error when that fails; otherwise it
// records the call and ends it as the next scripted result.
func (f *Function) Run(ctx context.Context, call port.FunctionCall) (crew.Verdict, error) {
	var settings FunctionSettings
	if call.Params != nil {
		if err := call.Params(&settings); err != nil {
			return "", err
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, FunctionRecord{Call: call, Settings: settings})
	var r FunctionResult
	if len(f.results) > 0 {
		r, f.results = f.results[0], f.results[1:]
	}
	f.mu.Unlock()
	if r.Print != "" && call.Log != nil {
		_, _ = io.WriteString(call.Log, r.Print)
	}
	switch {
	case r.Block:
		<-ctx.Done()
		return "", fmt.Errorf("the function was ended: %w", ctx.Err())
	case r.Err != nil:
		return "", r.Err
	case r.Verdict == "":
		return crew.Passed, nil
	default:
		return r.Verdict, nil
	}
}

// FunctionDefinition returns a definition declaring verdicts, whose factory
// decodes its parameters into FunctionSettings and records them, then
// returns the refusal Refuse set, if any, or else f itself.
func FunctionDefinition(f *Function, verdicts ...crew.Verdict) port.FunctionDefinition {
	return port.FunctionDefinition{
		Verdicts: verdicts,
		New: func(decode port.Decode) (port.Function, error) {
			var settings FunctionSettings
			if err := decode(&settings); err != nil {
				return nil, err
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			f.builds = append(f.builds, settings)
			if f.refusal != nil {
				return nil, *f.refusal
			}
			return f, nil
		},
	}
}
