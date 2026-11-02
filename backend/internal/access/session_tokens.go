package access

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

func sessionDigest(v string) string { d := sha256.Sum256([]byte(v)); return hex.EncodeToString(d[:]) }

func sessionEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
