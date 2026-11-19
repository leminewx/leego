package breaker

import "testing"

// TestStateString 验证状态名称的可读性与唯一性。
func TestStateString(t *testing.T) {
	cases := []struct {
		state State
		want  string
	}{
		{state: STATE_CLOSED, want: "CircuitBreaker.Closed"},
		{state: STATE_OPEN, want: "CircuitBreaker.Open"},
		{state: STATE_HALF_OPEN, want: "CircuitBreaker.HalfOpen"},
	}

	seen := make(map[string]struct{}, len(cases))
	for _, testCase := range cases {
		if got := testCase.state.String(); got != testCase.want {
			t.Fatalf("State.String() = %q, want %q", got, testCase.want)
		}

		if _, exists := seen[testCase.state.String()]; exists {
			t.Fatalf("状态名称重复: %q", testCase.state)
		}

		seen[testCase.state.String()] = struct{}{}
	}
}
