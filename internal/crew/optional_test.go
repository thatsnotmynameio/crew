package crew

import "testing"

func TestSomeGivesItsValue(t *testing.T) {
	if v, ok := Some(3).Get(); v != 3 || !ok {
		t.Errorf("Some(3).Get() = %v, %v, want 3, true", v, ok)
	}
	if v, ok := Some(0.0).Get(); v != 0 || !ok {
		t.Errorf("Some(0.0).Get() = %v, %v, want 0, true", v, ok)
	}
}

func TestTheZeroOptionalHasNoValue(t *testing.T) {
	if v, ok := (Optional[int]{}).Get(); v != 0 || ok {
		t.Errorf("Optional[int]{}.Get() = %v, %v, want 0, false", v, ok)
	}
	if v, ok := (Optional[string]{}).Get(); v != "" || ok {
		t.Errorf("Optional[string]{}.Get() = %q, %v, want \"\", false", v, ok)
	}
}

func TestOptionalsOfEqualValuesAreEqual(t *testing.T) {
	if Some(2.5) != Some(2.5) {
		t.Error("Some(2.5) != Some(2.5), want equal")
	}
	if Some(0) == (Optional[int]{}) {
		t.Error("Some(0) == the zero optional, want a value apart from none")
	}
	if Some(1) == Some(2) {
		t.Error("Some(1) == Some(2), want different")
	}
}
