package loom

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// Tests never touch the real OS keychain: CI machines have none, and a
// developer's own keychain must stay untouched.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
