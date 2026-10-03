package mates

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// redacted is how a private key prints.
const redacted = "[private key]"

// The app JWT's lifetime around the time it is made: issued a minute back,
// against clock drift, and expiring before GitHub's 10-minute limit.
const (
	jwtBackdate = 60 * time.Second
	jwtLife     = 9 * time.Minute
)

// PrivateKey is a mate's private key, a PEM. It never prints: every fmt
// verb, String and GoString show [private key], so a mate in a message or
// an error does not leak it. JSON still holds it, for the mate's file.
type PrivateKey string

// String returns [private key].
func (PrivateKey) String() string { return redacted }

// GoString returns [private key].
func (PrivateKey) GoString() string { return redacted }

// Format writes [private key] for every verb, so even a verb that does not
// call String, such as %d, cannot print the key.
func (PrivateKey) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(redacted))
}

// AppJWT returns the app JWT that authenticates as the app with the client
// id clientID, signed with key using RS256, issued 60 seconds before now and
// expiring 9 minutes after it. key is a PKCS#1 or PKCS#8 RSA key in PEM.
func AppJWT(key PrivateKey, clientID string, now time.Time) (string, error) {
	rsaKey, err := parseKey(key)
	if err != nil {
		return "", err
	}
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("app JWT header: %w", err)
	}
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-jwtBackdate).Unix(),
		"exp": now.Add(jwtLife).Unix(),
		"iss": clientID,
	})
	if err != nil {
		return "", fmt.Errorf("app JWT claims: %w", err)
	}
	signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(nil, rsaKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("sign the app JWT: %w", err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// parseKey reads key's first PEM block as a PKCS#1 RSA key, or else as a
// PKCS#8 one. Its errors never quote the key.
func parseKey(key PrivateKey) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(key))
	if block == nil {
		return nil, errors.New("the mate's private key is not a PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("the mate's private key is neither a PKCS#1 nor a PKCS#8 key")
	}
	k, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the mate's private key is not an RSA key")
	}
	return k, nil
}
