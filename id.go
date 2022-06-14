package stomp

import (
	"crypto/rand"
	"fmt"
)

// SessionID generates and returns a session SessionID.
func SessionID() string {
	b := make([]byte, 64)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
