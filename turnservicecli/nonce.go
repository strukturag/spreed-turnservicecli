package turnservicecli

import (
	"crypto/rand"
	"encoding/hex"
)

func makeNonce() (string, error) {
	nonce := make([]byte, 32)
	_, err := rand.Read(nonce)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce), nil
}
