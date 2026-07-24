package jtl

import "testing"

const header = "timeStamp,elapsed,label,responseCode,responseMessage,threadName,dataType,success,failureMessage,bytes,sentBytes,grpThreads,allThreads,URL,Latency,IdleTime,Connect"

func TestParseLineHeaderIsSkipped(t *testing.T) {
	_, ok, err := ParseLine(header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected header line to be skipped (ok=false)")
	}
}

func TestParseLineSample(t *testing.T) {
	line := "1690000000000,214,BoltRunner Request,200,OK,Thread Group 1-1,text,true,,1024,128,5,5,http://example.com/,210,0,3"
	s, ok, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a data line")
	}
	if s.TimestampMs != 1690000000000 || s.ElapsedMs != 214 || !s.Success {
		t.Fatalf("unexpected sample: %+v", s)
	}
}

func TestParseLineFailedSample(t *testing.T) {
	line := "1690000000000,214,BoltRunner Request,500,Error,Thread Group 1-1,text,false,,0,0,5,5,http://example.com/,210,0,3"
	s, ok, err := ParseLine(line)
	if err != nil || !ok {
		t.Fatalf("unexpected: ok=%v err=%v", ok, err)
	}
	if s.Success {
		t.Fatal("expected Success=false")
	}
}

func TestAggregate(t *testing.T) {
	samples := []Sample{
		{ElapsedMs: 100, Success: true},
		{ElapsedMs: 200, Success: true},
		{ElapsedMs: 300, Success: false},
		{ElapsedMs: 400, Success: true},
	}
	agg := Aggregate(samples, 2.0)

	if agg.SampleCount != 4 {
		t.Fatalf("expected 4 samples, got %d", agg.SampleCount)
	}
	if agg.ThroughputRPS != 2.0 {
		t.Fatalf("expected throughput 2.0 (4 samples / 2s), got %f", agg.ThroughputRPS)
	}
	if agg.AvgResponseTimeMs != 250.0 {
		t.Fatalf("expected avg response time 250.0, got %f", agg.AvgResponseTimeMs)
	}
	if agg.ErrorRatePct != 25.0 {
		t.Fatalf("expected error rate 25.0, got %f", agg.ErrorRatePct)
	}
}

func TestParseLineTooFewFields(t *testing.T) {
	_, ok, err := ParseLine("1690000000000,214,not,enough,fields")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected a short line to be skipped (ok=false)")
	}
}

func TestParseLineMalformedElapsed(t *testing.T) {
	line := "1690000000000,not-a-number,BoltRunner Request,200,OK,Thread Group 1-1,text,true"
	_, ok, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected a malformed elapsed field to be skipped (ok=false)")
	}
}

func TestAggregateEmpty(t *testing.T) {
	agg := Aggregate(nil, 1.0)
	if agg.SampleCount != 0 || agg.ThroughputRPS != 0 || agg.AvgResponseTimeMs != 0 || agg.ErrorRatePct != 0 {
		t.Fatalf("expected all-zero aggregate for no samples, got %+v", agg)
	}
}
