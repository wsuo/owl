package gbs

import "testing"

func TestContinuousMoveUsesRequestedSpeedAndStopIsZeroed(t *testing.T) {
	if got, want := BuildContinuousMove("left", 0.1), "a50f01021a1a00eb"; got != want {
		t.Fatalf("low speed command: got %s want %s", got, want)
	}
	if got, want := BuildStop(), "a50f0100000000b5"; got != want {
		t.Fatalf("stop command: got %s want %s", got, want)
	}
}
