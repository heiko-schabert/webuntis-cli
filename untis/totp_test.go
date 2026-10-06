package untis

import (
	"testing"
	"time"
)

func TestTOTP(t *testing.T) {
	// RFC 6238 appendix B, SHA-1, truncated to 6 digits.
	for sec, want := range map[int64]int{59: 287082, 1111111109: 81804, 2000000000: 279037} {
		got, err := totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", time.Unix(sec, 0))
		if err != nil || got != want {
			t.Errorf("t=%d: %d %v, want %d", sec, got, err, want)
		}
	}
}
