package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

// Typed BSON fields admit literal string values only. Neither a JSON operator
// object nor a user-selected query key can cross this boundary. Do not replace
// these types with a decoded request map, bson.Raw or JSON query interpolation.
type usernameLookup struct {
	Username string `bson:"username"`
}
type emailLookup struct {
	Email string `bson:"emails.address"`
}

func (s *service) findLoginUser(ctx context.Context, username, email string, byEmail bool) (user, error) {
	var selector any = usernameLookup{Username: username}
	if byEmail {
		selector = emailLookup{Email: email}
	}
	var u user
	// Constant BSON field names and typed string values, not executable query
	// text. TestLoginSelectorsRejectOperators covers hostile JSON/form inputs.
	err := s.db.Collection("users").FindOne(ctx, selector).Decode(&u)
	return u, err
}

// compareMeteorPassword verifies the complete existing Meteor password scheme:
// bcrypt(hex(SHA-256(password))). The salted, expensive bcrypt hash is the
// stored verifier. SHA-256 is only its compatibility prehash (also avoids bcrypt
// input truncation); it is never stored or used as a password verifier itself.
// This helper returns only the bcrypt comparison result, never the prehash.
func compareMeteorPassword(stored []byte, password string) bool {
	// CodeQL reports this intermediate hash without considering the mandatory
	// bcrypt comparison below. TestMeteorPasswordRequiresBcrypt pins that boundary.
	// See docs/Go-Login-Security.md for the reviewed CodeQL false positive.
	digest := sha256.Sum256([]byte(password))
	return bcrypt.CompareHashAndPassword(stored, []byte(hex.EncodeToString(digest[:]))) == nil
}
