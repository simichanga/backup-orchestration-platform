package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// authMiddleware requires a valid "Authorization: Bearer <token>" header on
// every request. Tokens are compared as SHA-256 hashes with
// crypto/subtle.ConstantTimeCompare rather than direct string equality, so
// comparison time depends only on the fixed 32-byte hash size, not on the
// presented token's length or a valid token's actual value.
func authMiddleware(tokenHashes [][32]byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}

		presented := sha256.Sum256([]byte(token))
		valid := false
		for _, h := range tokenHashes {
			if subtle.ConstantTimeCompare(presented[:], h[:]) == 1 {
				valid = true
				break
			}
		}
		if !valid {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func hashTokens(tokens []string) [][32]byte {
	hashes := make([][32]byte, len(tokens))
	for i, t := range tokens {
		hashes[i] = sha256.Sum256([]byte(t))
	}
	return hashes
}
