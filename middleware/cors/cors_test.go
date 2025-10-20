package cors

import (
	"testing"
)

func TestCors(t *testing.T) {
	cors := NewDefaultCORS("跨域中间件")
	t.Log(cors)
}
