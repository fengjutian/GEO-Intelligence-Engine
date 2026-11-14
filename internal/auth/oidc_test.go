package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOIDCVerifierValidatesRS256Token(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "test-key",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	defer jwks.Close()

	token := signRS256(t, key, Claims{
		Subject: "user-1", TenantID: "tenant-1", Role: "viewer",
		Issuer: "https://issuer.test", Audience: Audience{"geo-api"},
		Expires: time.Now().Add(time.Hour).Unix(),
	})
	verifier := NewOIDCVerifier("https://issuer.test", "geo-api", jwks.URL)
	claims, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.TenantID != "tenant-1" || claims.Role != "viewer" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestOIDCVerifierRejectsWrongAudienceAndKey(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "test-key",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	defer jwks.Close()
	verifier := NewOIDCVerifier("https://issuer.test", "geo-api", jwks.URL)
	claims := Claims{Subject: "u", TenantID: "t", Issuer: "https://issuer.test", Audience: Audience{"other"}, Expires: time.Now().Add(time.Hour).Unix()}
	if _, err := verifier.Verify(context.Background(), signRS256(t, key, claims)); err == nil {
		t.Fatal("wrong audience must be rejected")
	}
	claims.Audience = Audience{"geo-api"}
	if _, err := verifier.Verify(context.Background(), signRS256(t, other, claims)); err == nil {
		t.Fatal("wrong signing key must be rejected")
	}
}

func signRS256(t *testing.T, key *rsa.PrivateKey, claims Claims) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test-key"})
	payload, _ := json.Marshal(claims)
	encode := func(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }
	signingInput := encode(header) + "." + encode(payload)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + encode(signature)
}
