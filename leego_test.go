package leego

import (
	"regexp"
	"testing"
)

// TestReleaseMetadataConsistent 验证版本、年份与发布日期格式一致。
func TestReleaseMetadataConsistent(t *testing.T) {
	versionPattern := regexp.MustCompile(`^v\d+\.\d+\.\d+\.\d+$`)
	if !versionPattern.MatchString(version) {
		t.Fatalf("version = %q, want vX.Y.Z.W", version)
	}

	yearPattern := regexp.MustCompile(`^\d{4}$`)
	if !yearPattern.MatchString(year) {
		t.Fatalf("year = %q, want 四位年份", year)
	}

	releasePattern := regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}$`)
	if !releasePattern.MatchString(release) {
		t.Fatalf("release = %q, want YYYY.MM.DD", release)
	}

	if release[:4] != year {
		t.Fatalf("release 年份 %q 与 year %q 不一致", release[:4], year)
	}
}
