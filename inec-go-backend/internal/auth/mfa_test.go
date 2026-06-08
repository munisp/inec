package auth

import (
	"testing"
	"time"
)

func TestGenerateTOTP(t *testing.T) {
	m := &MFAService{digits: 6, period: 30}
	// RFC 6238 test vector (SHA1, time step 30s)
	// Using a known secret
	secret := "JBSWY3DPEHPK3PXP" // base32 encoded "Hello!"

	code := m.generateTOTP(secret, 1)
	if len(code) != 6 {
		t.Errorf("expected 6 digit code, got %d digits: %s", len(code), code)
	}

	// Same counter should produce same code
	code2 := m.generateTOTP(secret, 1)
	if code != code2 {
		t.Errorf("same counter produced different codes: %s vs %s", code, code2)
	}

	// Different counter should produce different code
	code3 := m.generateTOTP(secret, 2)
	if code == code3 {
		t.Errorf("different counters produced same code: %s", code)
	}
}

func TestValidateCode(t *testing.T) {
	m := &MFAService{digits: 6, period: 30}
	secret := "JBSWY3DPEHPK3PXP"

	now := time.Now()
	counter := uint64(now.Unix()) / 30
	expected := m.generateTOTP(secret, counter)

	// Current code should validate
	if !m.validateCode(secret, expected, now) {
		t.Error("current TOTP code should validate")
	}

	// Wrong code should not validate
	if m.validateCode(secret, "000000", now) {
		t.Error("wrong code should not validate")
	}

	// Previous step should also validate (clock drift tolerance)
	prevCode := m.generateTOTP(secret, counter-1)
	if !m.validateCode(secret, prevCode, now) {
		t.Error("previous step code should validate (clock drift)")
	}
}

func TestGenerateJTI(t *testing.T) {
	jti1 := generateJTI()
	jti2 := generateJTI()

	if jti1 == jti2 {
		t.Error("JTIs should be unique")
	}
	if len(jti1) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("expected 32 char JTI, got %d", len(jti1))
	}
}
