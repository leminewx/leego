package sse

import (
	"net/http/httptest"
	"testing"

	"gitee.com/leminewx/leego/response"
)

func TestMessage(t *testing.T) {
	msgEmpty := &Message{}
	resp := response.NewWriter(httptest.NewRecorder())

	if err := msgEmpty.Write(resp); err != nil {
		t.Error(err)
	}
	if len(msgEmpty.formatted) != 0 {
		t.Errorf("formtted data: %s", string(msgEmpty.formatted))
	}

	msgClosed := NewClosedMessage()
	if err := msgClosed.Write(resp); err != nil {
		t.Error(err)
	}
	if len(msgClosed.formatted) == 0 {
		t.Error("empty formtted data")
	}
}

// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		10536411	       145.6 ns/op	     305 B/op	       0 allocs/op
func BenchmarkMessage(b *testing.B) {
	msg := NewClosedMessage()
	resp := response.NewWriter(httptest.NewRecorder())

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if err := msg.Write(resp); err != nil {
			b.Error(err)
		}

		if len(msg.formatted) == 0 {
			b.Error("empty formtted data")
		}
	}
}
