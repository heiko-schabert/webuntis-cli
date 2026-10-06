package untis

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// totp is RFC 6238 with the authenticator defaults Untis uses: SHA-1, 30 s, 6 digits.
func totp(secret string, t time.Time) (int, error) {
	s := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return 0, fmt.Errorf("WEBUNTIS_SECRET is not base32: %w", err)
	}
	m := hmac.New(sha1.New, key)
	binary.Write(m, binary.BigEndian, uint64(t.Unix()/30))
	h := m.Sum(nil)
	o := h[len(h)-1] & 0xf
	return int(binary.BigEndian.Uint32(h[o:])&0x7fffffff) % 1_000_000, nil
}
