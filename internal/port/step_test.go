package port_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/port"
)

func TestStepReportsEachStepInOrder(t *testing.T) {
	var got []string
	ctx := port.WithSteps(context.Background(), func(step string) { got = append(got, step) })

	port.Step(ctx, "checking the gh login")
	port.Step(ctx, "finding the code owners")

	if want := []string{"checking the gh login", "finding the code owners"}; !reflect.DeepEqual(got, want) {
		t.Errorf("reported %q, want %q", got, want)
	}
}

func TestStepReachesTheReporterThroughDerivedContexts(t *testing.T) {
	var got []string
	ctx := port.WithSteps(context.Background(), func(step string) { got = append(got, step) })
	derived, cancel := context.WithCancel(ctx)
	defer cancel()

	port.Step(derived, "reading the run journal")

	if want := []string{"reading the run journal"}; !reflect.DeepEqual(got, want) {
		t.Errorf("reported %q, want %q", got, want)
	}
}

func TestStepOnAContextWithoutTheReporterReportsNothing(t *testing.T) {
	var got []string
	_ = port.WithSteps(context.Background(), func(step string) { got = append(got, step) })

	port.Step(context.Background(), "checking the gh login")

	if len(got) != 0 {
		t.Errorf("reported %q on a context without the reporter, want nothing", got)
	}
}

func TestStepOnAnEndedContextReportsNothing(t *testing.T) {
	var got []string
	ctx, cancel := context.WithCancel(port.WithSteps(context.Background(), func(step string) { got = append(got, step) }))
	cancel()

	port.Step(ctx, "looking for claude on PATH")

	if len(got) != 0 {
		t.Errorf("reported %q after the context ended, want nothing", got)
	}
}
