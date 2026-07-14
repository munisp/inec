package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sync"
)

// SecureRng is a cryptographically-seeded deterministic PRNG using HMAC-SHA256
// in counter mode. It replaces crypto/rand in security-sensitive biometric code.
// For deterministic use (same input → same output), seed from a hash of the input.
// For random use, seed from crypto/rand.
type SecureRng struct {
	key     []byte
	counter uint64
	buf     []byte
	pos     int
	mu      sync.Mutex
}

// NewSecureRng creates a SecureRng seeded from crypto/rand (fully unpredictable).
func NewSecureRng() *SecureRng {
	key := make([]byte, 32)
	rand.Read(key)
	return &SecureRng{key: key}
}

// NewSecureRngFromSeed creates a deterministic SecureRng from a seed.
// Same seed always produces the same sequence.
func NewSecureRngFromSeed(seed []byte) *SecureRng {
	h := sha256.Sum256(seed)
	return &SecureRng{key: h[:]}
}

func (s *SecureRng) fillBuf() {
	ctrBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(ctrBytes, s.counter)
	s.counter++
	mac := hmac.New(sha256.New, s.key)
	mac.Write(ctrBytes)
	s.buf = mac.Sum(nil)
	s.pos = 0
}

func (s *SecureRng) nextBytes(n int) []byte {
	result := make([]byte, n)
	filled := 0
	for filled < n {
		if s.pos >= len(s.buf) || s.buf == nil {
			s.fillBuf()
		}
		copied := copy(result[filled:], s.buf[s.pos:])
		s.pos += copied
		filled += copied
	}
	return result
}

// Intn returns a non-negative random int in [0, n).
func (s *SecureRng) Intn(n int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 {
		return 0
	}
	b := s.nextBytes(8)
	v := binary.BigEndian.Uint64(b)
	return int(v % uint64(n))
}

// Float64 returns a random float64 in [0.0, 1.0).
func (s *SecureRng) Float64() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.nextBytes(8)
	v := binary.BigEndian.Uint64(b) >> 11 // 53 bits
	return float64(v) / float64(1<<53)
}

// Int63 returns a non-negative random 63-bit integer.
func (s *SecureRng) Int63() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.nextBytes(8)
	v := binary.BigEndian.Uint64(b)
	return int64(v & 0x7FFFFFFFFFFFFFFF)
}

// NormFloat64 returns a normally distributed float64 (Box-Muller).
func (s *SecureRng) NormFloat64() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		b1 := s.nextBytes(8)
		b2 := s.nextBytes(8)
		u1 := float64(binary.BigEndian.Uint64(b1)>>11) / float64(1<<53)
		u2 := float64(binary.BigEndian.Uint64(b2)>>11) / float64(1<<53)
		if u1 < 1e-10 {
			continue
		}
		return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
	}
}
