package develop

import (
	"crypto/rand"
	"math/big"
)

// secretAlphabetLength is the size of the base62 alphabet app secrets draw
// from: digits, then lowercase, then uppercase.
const secretAlphabetLength = 62

// secretAlphabetAt maps an index in [0, 62) onto its base62 character.
func secretAlphabetAt(index int) byte {
	switch {
	case index < 10:
		return byte('0' + index)
	case index < 36:
		return byte('a' + index - 10)
	default:
		return byte('A' + index - 36)
	}
}

// generateSecret builds a cryptographically random base62 secret.
func generateSecret(length int) (string, error) {
	buffer := make([]byte, length)
	limit := big.NewInt(secretAlphabetLength)
	for index := range buffer {
		value, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		buffer[index] = secretAlphabetAt(int(value.Int64()))
	}
	return string(buffer), nil
}
