package core

import "testing"

func TestEstimateCost_Claude(t *testing.T) {
	tests := []struct {
		model    string
		wantMore float64 // cost should be > this
	}{
		{"claude-opus-4-6", 0},
		{"claude-sonnet-4-5", 0},
		{"claude-haiku-4-5", 0},
	}
	for _, tt := range tests {
		got := EstimateCost(tt.model, 1000, 500, 100, 200)
		if got <= tt.wantMore {
			t.Errorf("EstimateCost(%q, ...) = %f, want > %f", tt.model, got, tt.wantMore)
		}
	}
}

func TestEstimateCost_OpusMoreExpensive(t *testing.T) {
	opus := EstimateCost("claude-opus-4-6", 10000, 5000, 0, 0)
	sonnet := EstimateCost("claude-sonnet-4-5", 10000, 5000, 0, 0)
	haiku := EstimateCost("claude-haiku-4-5", 10000, 5000, 0, 0)

	if opus <= sonnet {
		t.Errorf("opus (%f) should be more expensive than sonnet (%f)", opus, sonnet)
	}
	if sonnet <= haiku {
		t.Errorf("sonnet (%f) should be more expensive than haiku (%f)", sonnet, haiku)
	}
}

func TestEstimateCost_Gemini(t *testing.T) {
	cost := EstimateCost("gemini-3-pro", 10000, 5000, 0, 0)
	if cost <= 0 {
		t.Errorf("Gemini cost should be > 0, got %f", cost)
	}
}

func TestEstimateCost_GPT4(t *testing.T) {
	cost4o := EstimateCost("gpt-4o-2025", 10000, 5000, 0, 0)
	cost4 := EstimateCost("gpt-4-turbo", 10000, 5000, 0, 0)

	if cost4o <= 0 {
		t.Errorf("GPT-4o cost should be > 0, got %f", cost4o)
	}
	if cost4 <= 0 {
		t.Errorf("GPT-4 cost should be > 0, got %f", cost4)
	}
	if cost4 <= cost4o {
		t.Errorf("GPT-4 (%f) should be more expensive than GPT-4o (%f)", cost4, cost4o)
	}
}

func TestEffectiveCost_PrefersDirect(t *testing.T) {
	direct := 0.05
	got := EffectiveCost("claude-sonnet-4-5", direct, 100000, 50000, 0, 0)
	if got != direct {
		t.Errorf("EffectiveCost should return direct cost %f, got %f", direct, got)
	}
}

func TestEffectiveCost_FallsBackToEstimate(t *testing.T) {
	got := EffectiveCost("claude-sonnet-4-5", 0, 10000, 5000, 0, 0)
	if got <= 0 {
		t.Errorf("EffectiveCost should estimate when direct cost is 0, got %f", got)
	}
}
