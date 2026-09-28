package identity

import "github.com/postie-labs/go-postie-lib/crypto"

// NewDemoKey creates an identity for one CLI process. Nicknames are display
// labels and must not be used as key material.
func NewDemoKey() (*crypto.PrivKey, error) {
	return crypto.GenPrivKey()
}
