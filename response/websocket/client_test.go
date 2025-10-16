package websocket

import (
	"fmt"
	"testing"
)

func TestNilChan(t *testing.T) {
	var ch chan int
	fmt.Println(ch == nil)
	select {
	case <-ch:
	default:
	}
}
