// Package auth resolves an API key to a tenant.
//
// The same shape as nine-billing: the plaintext key is never stored, only its
// SHA-256 digest, so a leaked key table yields nothing usable. For v0 the key
// table is in memory and seeded from the environment; it moves to Postgres
// when nine-core owns tenants.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
)

var ErrUnknownKey = errors.New("unknown or revoked api key")

type Keys struct {
	mu     sync.RWMutex
	byHash map[string]string // key digest -> tenant id
}

func NewKeys() *Keys {
	return &Keys{byHash: map[string]string{}}
}

// FromEnv seeds keys from NINE_INGEST_KEYS, formatted "tenant:key,tenant:key".
// Development convenience with a production shape: the process holds digests,
// not keys, from the moment it starts.
func FromEnv() *Keys {
	k := NewKeys()
	for _, pair := range strings.Split(os.Getenv("NINE_INGEST_KEYS"), ",") {
		tenant, key, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if ok && tenant != "" && key != "" {
			k.Add(tenant, key)
		}
	}
	return k
}

func (k *Keys) Add(tenant, plaintext string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.byHash[Hash(plaintext)] = tenant
}

// Resolve compares in constant time so the lookup does not leak key material
// through timing.
func (k *Keys) Resolve(plaintext string) (string, error) {
	if plaintext == "" {
		return "", ErrUnknownKey
	}
	want := Hash(plaintext)

	k.mu.RLock()
	defer k.mu.RUnlock()
	for h, tenant := range k.byHash {
		if subtle.ConstantTimeCompare([]byte(h), []byte(want)) == 1 {
			return tenant, nil
		}
	}
	return "", ErrUnknownKey
}

func Hash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
