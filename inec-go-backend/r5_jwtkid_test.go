// R5-047 regression tests: JWT kid header + key rotation (accept
// current+previous, reject unknown kid, emit kid on signing).
package main

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestR5047_TokensCarryKIDAndVerify(t *testing.T) {
	tok, err := createAccessToken(map[string]interface{}{"sub": "1", "role": "admin"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(tok, func(t *jwt.Token) (interface{}, error) { return jwtCurrent.key, nil })
	if err != nil {
		t.Fatalf("current-key token must verify: %v", err)
	}
	if parsed.Header["kid"] != jwtCurrent.kid {
		t.Fatalf("token must carry kid header, got %v", parsed.Header["kid"])
	}
	if _, err := decodeToken(tok); err != nil {
		t.Fatalf("decodeToken must accept current kid: %v", err)
	}
}

func TestR5047_PreviousKeyAcceptedDuringRotation(t *testing.T) {
	// Simulate: old key moves to JWT_SECRET_PREVIOUS, new key is current.
	oldKey := []byte("old-rotation-key-material-32-bytes!!!")
	prevSaved := jwtPrevious
	curSaved := jwtCurrent
	defer func() { jwtPrevious, jwtCurrent = prevSaved, curSaved }()

	old := &jwtKey{kid: jwtKID(oldKey), key: oldKey}
	legacyToken, err := func() (string, error) {
		mc := jwt.MapClaims{"sub": "1", "role": "admin", "exp": 9999999999, "type": "access"}
		tk := jwt.NewWithClaims(jwt.SigningMethodHS256, mc)
		tk.Header["kid"] = old.kid
		return tk.SignedString(oldKey)
	}()
	if err != nil {
		t.Fatal(err)
	}

	// Without the previous key configured, the legacy token fails closed.
	jwtPrevious = nil
	if _, err := decodeToken(legacyToken); err == nil {
		t.Fatal("unknown kid must be rejected when no previous key is configured")
	}

	// During the rotation window it verifies.
	jwtPrevious = old
	if _, err := decodeToken(legacyToken); err != nil {
		t.Fatalf("previous-key token must verify during rotation window: %v", err)
	}
}

func TestR5047_ForgedKIDRejected(t *testing.T) {
	evilKey := []byte("attacker-controlled-key-material-32!!")
	mc := jwt.MapClaims{"sub": "1", "role": "admin", "exp": 9999999999, "type": "access"}
	tk := jwt.NewWithClaims(jwt.SigningMethodHS256, mc)
	tk.Header["kid"] = jwtKID(evilKey)
	forged, err := tk.SignedString(evilKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeToken(forged); err == nil {
		t.Fatal("token with attacker kid/key must be rejected")
	}
}
