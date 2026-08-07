package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Key builds a stable idempotency key for side effects.
func Key(applicationID, action, slotID string) string {
	raw := fmt.Sprintf("%s|%s|%s", applicationID, action, slotID)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}
