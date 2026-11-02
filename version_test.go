package rosetta

import (
	"regexp"
	"testing"
)

func TestVersionIsSemantic(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Fatalf("Version = %q, want MAJOR.MINOR.PATCH from the VERSION file", Version)
	}
}
