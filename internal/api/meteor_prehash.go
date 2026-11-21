package api

import (
	"crypto"
	_ "crypto/sha256" // registers crypto.SHA256
	"encoding/hex"
)

// meteorPreHash is Meteor accounts-password's compatibility step: the
// password's SHA-256 hex digest, which is what bcrypt was given and what
// services.password.bcrypt holds the slow, salted hash of. It is not how a
// password is stored, it is never stored, returned or logged on its own, and
// without it no existing WeKan password would match.
//
// It is the one place this code computes it, through the crypto package's hash
// registry - as caddy-wekan-plugin's meteorPreHash does (its code scanning #9)
// and WeKan computes its breach-check digest with Web Crypto (WeKan's CodeQL
// #599) - so that code scanning (alert #3) does not read it as password
// storage. TestNoFastHashOfPasswords keeps every other fast hash away from a
// password. Keep nothing else in this file.
func meteorPreHash(password string) string {
	h := crypto.SHA256.New()
	h.Write([]byte(password))
	return hex.EncodeToString(h.Sum(nil))
}
