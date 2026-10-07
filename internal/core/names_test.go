package core_test

import (
	"fmt"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
)

// nameCases are the names renderers show for the core's enumerations,
// including a value out of range.
var nameCases = []struct {
	value fmt.Stringer
	want  string
}{
	{core.CallMove, "move"},
	{core.CallReport, "report"},
	{core.CallPullRequests, "pull requests"},
	{core.CallComment, "comment"},
	{core.CallClose, "close"},
	{core.CallKind(99), "move"},
	{core.ClaimTaking, "taking"},
	{core.ClaimRunning, "running"},
	{core.ClaimStopping, "stopping"},
	{core.ClaimJudging, "judging"},
	{core.ClaimOwed, "owed"},
	{core.Claim(99), "unknown"},
	{core.PhaseWaiting, "waiting"},
	{core.PhaseCreating, "creating workspace"},
	{core.PhaseReopening, "reopening workspace"},
	{core.PhaseStarting, "starting"},
	{core.PhaseRunning, "running"},
	{core.PhaseChecking, "checking"},
	{core.PhaseEnded, "ended"},
	{core.Phase(99), "unknown"},
	{core.ResultDone, "done"},
	{core.ResultMovedMeanwhile, "moved meanwhile"},
	{core.ResultRefused, "refused"},
	{core.ResultFailed, "failed"},
	{core.Result(99), "unknown"},
}

func TestEnumerationsNameThemselvesForRenderers(t *testing.T) {
	for _, tc := range nameCases {
		if got := tc.value.String(); got != tc.want {
			t.Errorf("%T(%d).String() = %q, want %q", tc.value, tc.value, got, tc.want)
		}
	}
}
