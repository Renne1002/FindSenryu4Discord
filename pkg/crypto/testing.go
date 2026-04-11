package crypto

import "sync"

// ResetForTest resets the crypto package state for test isolation.
// This function must only be called from tests.
func ResetForTest() {
	gcm = nil
	enabled = false
	once = sync.Once{}
}
