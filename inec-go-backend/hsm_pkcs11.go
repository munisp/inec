package main

// Real PKCS#11 Hardware Security Module backend.
//
// This provides genuine HSM-backed ECDSA signing via the standard PKCS#11
// interface. It works against any PKCS#11 v2.x module, including:
//   - SoftHSM2         (software token, for CI/testing)      HSM_PKCS11_LIB=/usr/lib/softhsm/libsofthsm2.so
//   - AWS CloudHSM     (Cavium)                              HSM_PKCS11_LIB=/opt/cloudhsm/lib/libcloudhsm_pkcs11.so
//   - Thales Luna      (SafeNet)                             HSM_PKCS11_LIB=/usr/lib/libCryptoki2_64.so
//   - YubiHSM2 / Nitrokey (via their PKCS#11 modules)
//
// The private key never leaves the token: signing is performed on the device
// via C_Sign(CKM_ECDSA). When no module is configured the caller falls back to
// the in-process software key (ProductionHSM.ecdsaKey).
//
// Configuration (environment):
//   HSM_PKCS11_LIB          path to the PKCS#11 shared object (required to enable)
//   HSM_PKCS11_PIN          user PIN for the token slot
//   HSM_PKCS11_TOKEN_LABEL  select the slot whose token has this label (optional)
//   HSM_PKCS11_KEY_LABEL    signing key label (default: INEC_SIGNING_KEY)

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/asn1"
	"fmt"
	"math/big"
	"os"

	"github.com/miekg/pkcs11"
	"github.com/rs/zerolog/log"
)

// PKCS11Signer holds an open, authenticated session to a PKCS#11 token and a
// handle to a persistent EC signing key on that token.
type PKCS11Signer struct {
	ctx       *pkcs11.Ctx
	session   pkcs11.SessionHandle
	privitem  pkcs11.ObjectHandle
	pub       *ecdsa.PublicKey
	keyLabel  string
	tokenInfo string
}

// NewPKCS11Signer initialises the module, logs into the token, and locates (or
// generates) the persistent P-256 signing key identified by keyLabel.
func NewPKCS11Signer() (*PKCS11Signer, error) {
	lib := os.Getenv("HSM_PKCS11_LIB")
	if lib == "" {
		return nil, fmt.Errorf("HSM_PKCS11_LIB not set")
	}
	pin := os.Getenv("HSM_PKCS11_PIN")
	keyLabel := envOrDefault("HSM_PKCS11_KEY_LABEL", "INEC_SIGNING_KEY")
	tokenLabel := os.Getenv("HSM_PKCS11_TOKEN_LABEL")

	ctx := pkcs11.New(lib)
	if ctx == nil {
		return nil, fmt.Errorf("failed to load PKCS#11 module %q", lib)
	}
	if err := ctx.Initialize(); err != nil {
		ctx.Destroy()
		return nil, fmt.Errorf("C_Initialize: %w", err)
	}

	slots, err := ctx.GetSlotList(true)
	if err != nil || len(slots) == 0 {
		ctx.Finalize()
		ctx.Destroy()
		return nil, fmt.Errorf("no PKCS#11 slots with a token present: %v", err)
	}

	slot := slots[0]
	if tokenLabel != "" {
		found := false
		for _, s := range slots {
			ti, terr := ctx.GetTokenInfo(s)
			if terr == nil && ti.Label == tokenLabel {
				slot = s
				found = true
				break
			}
		}
		if !found {
			ctx.Finalize()
			ctx.Destroy()
			return nil, fmt.Errorf("no token with label %q", tokenLabel)
		}
	}

	session, err := ctx.OpenSession(slot, pkcs11.CKF_SERIAL_SESSION|pkcs11.CKF_RW_SESSION)
	if err != nil {
		ctx.Finalize()
		ctx.Destroy()
		return nil, fmt.Errorf("C_OpenSession: %w", err)
	}
	if pin != "" {
		if err := ctx.Login(session, pkcs11.CKU_USER, pin); err != nil {
			// CKR_USER_ALREADY_LOGGED_IN (0x100) is benign.
			if perr, ok := err.(pkcs11.Error); !ok || perr != pkcs11.CKR_USER_ALREADY_LOGGED_IN {
				ctx.CloseSession(session)
				ctx.Finalize()
				ctx.Destroy()
				return nil, fmt.Errorf("C_Login: %w", err)
			}
		}
	}

	s := &PKCS11Signer{ctx: ctx, session: session, keyLabel: keyLabel}
	ti, _ := ctx.GetTokenInfo(slot)
	s.tokenInfo = fmt.Sprintf("%s (%s)", ti.Label, ti.ManufacturerID)

	priv, pubHandle, err := s.findKeyPair(keyLabel)
	if err != nil {
		// Key not present yet: generate a persistent P-256 keypair on the token.
		priv, pubHandle, err = s.generateKeyPair(keyLabel)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("no signing key and generation failed: %w", err)
		}
		log.Info().Str("label", keyLabel).Msg("PKCS#11: generated new EC P-256 signing key on token")
	}
	s.privitem = priv

	pub, err := s.readPublicKey(pubHandle)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("read EC public key: %w", err)
	}
	s.pub = pub

	log.Info().Str("token", s.tokenInfo).Str("key", keyLabel).Msg("PKCS#11 HSM signer ready")
	return s, nil
}

func (s *PKCS11Signer) findKeyPair(label string) (priv, pub pkcs11.ObjectHandle, err error) {
	priv, err = s.findObject([]*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PRIVATE_KEY),
		pkcs11.NewAttribute(pkcs11.CKA_LABEL, label),
	})
	if err != nil {
		return 0, 0, err
	}
	pub, err = s.findObject([]*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PUBLIC_KEY),
		pkcs11.NewAttribute(pkcs11.CKA_LABEL, label),
	})
	if err != nil {
		return 0, 0, err
	}
	return priv, pub, nil
}

func (s *PKCS11Signer) findObject(template []*pkcs11.Attribute) (pkcs11.ObjectHandle, error) {
	if err := s.ctx.FindObjectsInit(s.session, template); err != nil {
		return 0, err
	}
	objs, _, err := s.ctx.FindObjects(s.session, 1)
	s.ctx.FindObjectsFinal(s.session)
	if err != nil {
		return 0, err
	}
	if len(objs) == 0 {
		return 0, fmt.Errorf("object not found")
	}
	return objs[0], nil
}

// generateKeyPair creates a persistent, non-extractable P-256 signing key.
func (s *PKCS11Signer) generateKeyPair(label string) (priv, pub pkcs11.ObjectHandle, err error) {
	// OID for prime256v1 / secp256r1 (P-256): 1.2.840.10045.3.1.7
	ecParams, _ := asn1.Marshal(asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7})

	pubTemplate := []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PUBLIC_KEY),
		pkcs11.NewAttribute(pkcs11.CKA_KEY_TYPE, pkcs11.CKK_EC),
		pkcs11.NewAttribute(pkcs11.CKA_TOKEN, true),
		pkcs11.NewAttribute(pkcs11.CKA_VERIFY, true),
		pkcs11.NewAttribute(pkcs11.CKA_EC_PARAMS, ecParams),
		pkcs11.NewAttribute(pkcs11.CKA_LABEL, label),
	}
	privTemplate := []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PRIVATE_KEY),
		pkcs11.NewAttribute(pkcs11.CKA_KEY_TYPE, pkcs11.CKK_EC),
		pkcs11.NewAttribute(pkcs11.CKA_TOKEN, true),
		pkcs11.NewAttribute(pkcs11.CKA_PRIVATE, true),
		pkcs11.NewAttribute(pkcs11.CKA_SENSITIVE, true),
		pkcs11.NewAttribute(pkcs11.CKA_EXTRACTABLE, false),
		pkcs11.NewAttribute(pkcs11.CKA_SIGN, true),
		pkcs11.NewAttribute(pkcs11.CKA_LABEL, label),
	}
	// C_GenerateKeyPair returns (publicHandle, privateHandle); map to our
	// (priv, pub) return order.
	pubH, privH, gerr := s.ctx.GenerateKeyPair(s.session,
		[]*pkcs11.Mechanism{pkcs11.NewMechanism(pkcs11.CKM_EC_KEY_PAIR_GEN, nil)},
		pubTemplate, privTemplate)
	return privH, pubH, gerr
}

// readPublicKey extracts CKA_EC_POINT and decodes it to an ecdsa.PublicKey.
func (s *PKCS11Signer) readPublicKey(pubHandle pkcs11.ObjectHandle) (*ecdsa.PublicKey, error) {
	attrs, err := s.ctx.GetAttributeValue(s.session, pubHandle,
		[]*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_EC_POINT, nil)})
	if err != nil {
		return nil, err
	}
	raw := attrs[0].Value

	// CKA_EC_POINT is a DER-encoded OCTET STRING wrapping the point; some
	// modules return the raw point directly. Handle both.
	point := raw
	var wrapped []byte
	if _, uerr := asn1.Unmarshal(raw, &wrapped); uerr == nil && len(wrapped) > 0 {
		point = wrapped
	}

	x, y := elliptic.Unmarshal(elliptic.P256(), point)
	if x == nil {
		return nil, fmt.Errorf("failed to decode EC point (%d bytes)", len(point))
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
}

// Sign performs on-device ECDSA over the given digest. It returns the raw r||s
// concatenation (64 bytes for P-256), matching ProductionHSM's hex encoding.
func (s *PKCS11Signer) Sign(digest []byte) ([]byte, error) {
	if err := s.ctx.SignInit(s.session,
		[]*pkcs11.Mechanism{pkcs11.NewMechanism(pkcs11.CKM_ECDSA, nil)}, s.privitem); err != nil {
		return nil, fmt.Errorf("C_SignInit: %w", err)
	}
	sig, err := s.ctx.Sign(s.session, digest)
	if err != nil {
		return nil, fmt.Errorf("C_Sign: %w", err)
	}
	return sig, nil
}

// PublicKey returns the token's signing public key.
func (s *PKCS11Signer) PublicKey() *ecdsa.PublicKey { return s.pub }

// Verify checks a raw r||s signature against the token public key.
func (s *PKCS11Signer) Verify(digest, sig []byte) bool {
	if s.pub == nil || len(sig) < 2 {
		return false
	}
	half := len(sig) / 2
	r := new(big.Int).SetBytes(sig[:half])
	ss := new(big.Int).SetBytes(sig[half:])
	return ecdsa.Verify(s.pub, digest, r, ss)
}

// Close logs out and releases the module.
func (s *PKCS11Signer) Close() {
	if s.ctx == nil {
		return
	}
	s.ctx.Logout(s.session)
	s.ctx.CloseSession(s.session)
	s.ctx.Finalize()
	s.ctx.Destroy()
	s.ctx = nil
}
