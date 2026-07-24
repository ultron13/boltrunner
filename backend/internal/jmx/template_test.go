package jmx

import "testing"

func TestGenerateContainsParams(t *testing.T) {
	out, err := Generate(Params{TargetURL: "http://example.com/path", VirtualUsers: 25, DurationSeconds: 60})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	mustContain := []string{
		`<stringProp name="ThreadGroup.num_threads">25</stringProp>`,
		`<stringProp name="HTTPSampler.domain">example.com</stringProp>`,
		`<stringProp name="HTTPSampler.path">/path</stringProp>`,
		`<stringProp name="ThreadGroup.duration">60</stringProp>`,
	}
	for _, want := range mustContain {
		if !contains(out, want) {
			t.Fatalf("expected generated jmx to contain %q\n---\n%s", want, out)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestGenerateRejectsInvalidURL(t *testing.T) {
	_, err := Generate(Params{TargetURL: "not-a-url", VirtualUsers: 1, DurationSeconds: 1})
	if err == nil {
		t.Fatal("expected an error for an invalid target URL")
	}
}
