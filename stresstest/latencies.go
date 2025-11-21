package stress

import (
	"time"
)

type Latencies struct {
	latencies []time.Duration
}

func NewLatencies() *Latencies {
	return &Latencies{latencies: make([]time.Duration, 0)}
}

func NewLatenciesWithCapacity(capacity int) *Latencies {
	return &Latencies{latencies: make([]time.Duration, 0, capacity)}
}

func (own *Latencies) Append(latency time.Duration) {
	own.latencies = append(own.latencies, latency)
}

func (own *Latencies) GetLatencies() []time.Duration {
	return own.latencies
}

func (own *Latencies) GetPercentile(percentile int) time.Duration {
	if percentile < 1 {
		percentile = 0
	}
	if percentile > 100 {
		percentile = 100
	}

	return own.latencies[own.Len()*percentile/100]
}

func (own *Latencies) CountTotalLatency() time.Duration {
	var total time.Duration
	for _, duration := range own.latencies {
		total += duration
	}

	return total
}

func (own *Latencies) Len() int {
	return len(own.latencies)
}

func (own *Latencies) Less(i, j int) bool {
	return (own.latencies)[i] < (own.latencies)[j]
}

func (own *Latencies) Swap(i, j int) {
	latencies := own.latencies
	(latencies)[i], (latencies)[j] = (latencies)[j], (latencies)[i]
}
