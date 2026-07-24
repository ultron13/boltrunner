package jtl

import (
	"strconv"
	"strings"
)

type Sample struct {
	TimestampMs int64
	ElapsedMs   int64
	Success     bool
}

// ParseLine parses one line of a JMeter CSV .jtl file. The second return value
// is false for the header line (or any unparseable line), never an error, so
// callers tailing a live file can simply skip non-sample lines.
func ParseLine(line string) (Sample, bool, error) {
	fields := strings.Split(strings.TrimRight(line, "\r\n"), ",")
	if len(fields) < 8 {
		return Sample{}, false, nil
	}
	ts, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return Sample{}, false, nil // header or malformed line
	}
	elapsed, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return Sample{}, false, nil
	}
	success := fields[7] == "true"
	return Sample{TimestampMs: ts, ElapsedMs: elapsed, Success: success}, true, nil
}

type AggregateResult struct {
	ThroughputRPS     float64
	AvgResponseTimeMs float64
	ErrorRatePct      float64
	SampleCount       int
}

// Aggregate computes rolling metrics for a batch of samples collected over windowSeconds.
func Aggregate(samples []Sample, windowSeconds float64) AggregateResult {
	if len(samples) == 0 {
		return AggregateResult{}
	}
	var totalElapsed int64
	var failed int
	for _, s := range samples {
		totalElapsed += s.ElapsedMs
		if !s.Success {
			failed++
		}
	}
	n := len(samples)
	result := AggregateResult{
		SampleCount:       n,
		AvgResponseTimeMs: float64(totalElapsed) / float64(n),
		ErrorRatePct:      float64(failed) / float64(n) * 100,
	}
	if windowSeconds > 0 {
		result.ThroughputRPS = float64(n) / windowSeconds
	}
	return result
}
