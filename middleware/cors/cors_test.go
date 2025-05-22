package cors

import "testing"

func TestCors(t *testing.T) {
	cors := New()
	t.Log(cors)
}
