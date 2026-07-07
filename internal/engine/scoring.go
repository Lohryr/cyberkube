// Package engine implements the challenge catalog, flag submission, dynamic
// instance lifecycle, and scoring — the CTFd-equivalent domain logic, driven
// entirely by Challenge CRs.
package engine

import (
	"math"

	"github.com/CyberKube-ISEN/cyberkube/internal/k8s"
)

// CurrentValue computes a challenge's present point value given how many
// teams have already solved it. Decay never brings the value below Minimum,
// and previously awarded points are never recomputed (they are recorded at
// solve time).
func CurrentValue(ch *k8s.Challenge, solveCount int) int {
	initial := ch.Initial
	if initial <= 0 {
		initial = ch.Value
	}
	if initial <= 0 {
		return 0
	}
	if ch.Decay <= 0 || solveCount <= 0 {
		return initial
	}

	var value float64
	switch ch.Function {
	case "logarithmic":
		value = float64(initial) - float64(ch.Decay)*math.Log2(float64(solveCount)+1)
	default: // linear
		value = float64(initial) - float64(ch.Decay)*float64(solveCount)
	}

	minimum := ch.Minimum
	if minimum <= 0 {
		minimum = 1
	}
	if value < float64(minimum) {
		return minimum
	}
	return int(math.Round(value))
}
