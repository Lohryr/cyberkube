package engine

import (
	"testing"

	"github.com/CyberKube-ISEN/cyberkube/internal/k8s"
)

func TestCurrentValueNoDecayBeforeSolves(t *testing.T) {
	ch := &k8s.Challenge{Initial: 500, Decay: 15, Minimum: 75, Function: "linear"}
	if got := CurrentValue(ch, 0); got != 500 {
		t.Errorf("CurrentValue(0 solves) = %d, want 500", got)
	}
}

func TestCurrentValueLinearDecay(t *testing.T) {
	ch := &k8s.Challenge{Initial: 500, Decay: 15, Minimum: 75, Function: "linear"}
	// 500 - 15*10 = 350
	if got := CurrentValue(ch, 10); got != 350 {
		t.Errorf("CurrentValue(10 solves) = %d, want 350", got)
	}
}

func TestCurrentValueNeverBelowMinimum(t *testing.T) {
	ch := &k8s.Challenge{Initial: 500, Decay: 100, Minimum: 75, Function: "linear"}
	// 500 - 100*10 would be negative; clamps to 75
	if got := CurrentValue(ch, 10); got != 75 {
		t.Errorf("CurrentValue(10 solves) = %d, want 75 (minimum)", got)
	}
}

func TestCurrentValueLogarithmic(t *testing.T) {
	ch := &k8s.Challenge{Initial: 500, Decay: 50, Minimum: 50, Function: "logarithmic"}
	// 500 - 50*log2(4) = 500 - 100 = 400 at 3 solves
	if got := CurrentValue(ch, 3); got != 400 {
		t.Errorf("CurrentValue(3 solves, log) = %d, want 400", got)
	}
}

func TestCurrentValueFallsBackToStaticValue(t *testing.T) {
	ch := &k8s.Challenge{Value: 100} // no Initial/Decay: static-scored
	if got := CurrentValue(ch, 42); got != 100 {
		t.Errorf("CurrentValue(static) = %d, want 100", got)
	}
}
