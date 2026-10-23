package develop

import "github.com/google/uuid"

// newUUID returns a fresh identifier for a domain record.
func newUUID() string { return uuid.NewString() }

// validUUID reports whether a value is a canonical UUID.
func validUUID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}
