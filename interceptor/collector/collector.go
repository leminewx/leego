package collector

import (
	"sync/atomic"
)

type Collector struct {
	Total    int64 `json:"total" xml:"total"`
	InFlight int64 `json:"in_flight" xml:"in_flight"`
}

func New() *Collector {
	return &Collector{}
}

func (own *Collector) Increase() {
	atomic.AddInt64(&own.InFlight, 1)
}

func (own *Collector) Decrease() {
	atomic.AddInt64(&own.Total, 1)
	atomic.AddInt64(&own.InFlight, -1)
}
