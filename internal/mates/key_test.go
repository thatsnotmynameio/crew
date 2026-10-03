package mates

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// testRSAKey is one RSA key per test binary: generating one takes a while.
var testRSAKey = sync.OnceValue(func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
})

// pkcs1Key returns the test key as a PKCS#1 PEM, the form GitHub hands out.
func pkcs1Key() PrivateKey {
	der := x509.MarshalPKCS1PrivateKey(testRSAKey())
	return PrivateKey(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
}

// pkcs8Key returns key as a PKCS#8 PEM.
func pkcs8Key(t *testing.T, key any) PrivateKey {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal PKCS#8: %v", err)
	}
	return PrivateKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// verifyJWT checks token's RS256 signature against the test key and returns
// its header and claims.
func verifyJWT(t *testing.T, token string) (map[string]any, map[string]any) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&testRSAKey().PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	decoded := make([]map[string]any, 2)
	for i := range decoded {
		raw, err := base64.RawURLEncoding.DecodeString(parts[i])
		if err != nil {
			t.Fatalf("decode part %d: %v", i, err)
		}
		if err := json.Unmarshal(raw, &decoded[i]); err != nil {
			t.Fatalf("unmarshal part %d: %v", i, err)
		}
	}
	return decoded[0], decoded[1]
}

// checkClaims checks that claims are an app JWT's for the client id
// Iv23client, made at now.
func checkClaims(t *testing.T, claims map[string]any, now time.Time) {
	t.Helper()
	if claims["iss"] != "Iv23client" {
		t.Errorf("iss = %v, want the client id", claims["iss"])
	}
	iat, _ := claims["iat"].(float64)
	exp, _ := claims["exp"].(float64)
	if int64(iat) != now.Unix()-60 {
		t.Errorf("iat = %v, want 60 seconds before %d", iat, now.Unix())
	}
	if life := int64(exp) - now.Unix(); life <= 0 || life >= 600 {
		t.Errorf("exp is %d seconds after now, want under 10 minutes", life)
	}
}

func TestAppJWTIsSignedWithRS256ForTheClientID(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for name, key := range map[string]PrivateKey{"PKCS#1": pkcs1Key(), "PKCS#8": pkcs8Key(t, testRSAKey())} {
		t.Run(name, func(t *testing.T) {
			token, err := AppJWT(key, "Iv23client", now)
			if err != nil {
				t.Fatalf("AppJWT: %v", err)
			}
			header, claims := verifyJWT(t, token)
			if header["alg"] != "RS256" || header["typ"] != "JWT" {
				t.Errorf("header = %v, want RS256 JWT", header)
			}
			checkClaims(t, claims, now)
		})
	}
}

func TestAppJWTRejectsWhatIsNotAnRSAKey(t *testing.T) {
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	garbage := PrivateKey(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("garbage")}))
	for name, key := range map[string]PrivateKey{
		"no PEM":      "not a key at all",
		"garbage PEM": garbage,
		"ed25519":     pkcs8Key(t, edKey),
	} {
		if _, err := AppJWT(key, "Iv23client", time.Now()); err == nil {
			t.Errorf("AppJWT(%s) = nil error, want one", name)
		}
	}
}

func TestPrivateKeyNeverPrints(t *testing.T) {
	key := pkcs1Key()
	m := Mate{Name: "tester", Owner: "thatsnotmynameio", AppID: 1, PrivateKey: key}
	body := strings.Split(string(key), "\n")[1]
	outs := map[string]string{
		"%v of the mate": fmt.Sprintf("%v", m), "%v of its pointer": fmt.Sprintf("%v", &m),
		"String": key.String(), "GoString": key.GoString(),
		"a wrapping error": fmt.Errorf("save %v: %w", m, errors.New("boom")).Error(),
	}
	for _, verb := range []string{"%+v", "%#v", "%s", "%q", "%x", "%d"} {
		outs[verb+" of the mate"] = fmt.Sprintf(verb, m)
		outs[verb+" of the key"] = fmt.Sprintf(verb, key)
	}
	for how, out := range outs {
		// The message never echoes out: a failure must not print the key.
		if strings.Contains(out, "PRIVATE KEY") || strings.Contains(out, body) {
			t.Errorf("%s printed the key", how)
		}
	}
	if got := outs["%v of the mate"]; !strings.Contains(got, "[private key]") {
		t.Errorf("%%v of the mate shows no [private key]")
	}
}
