package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OIDCVerifier validates RS256 ID/access tokens against an issuer JWKS.
type OIDCVerifier struct {
	Issuer   string
	Audience string
	JWKSURL  string
	Client   *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func NewOIDCVerifier(issuer, audience, jwksURL string) *OIDCVerifier {
	return &OIDCVerifier{Issuer: issuer, Audience: audience, JWKSURL: jwksURL, Client: &http.Client{Timeout: 10 * time.Second}, keys: map[string]*rsa.PublicKey{}}
}

// Verify validates an RS256 token, its issuer/audience and expiry.
func (v *OIDCVerifier) Verify(ctx context.Context, token string) (Claims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil || !strings.EqualFold(header.Algorithm, "RS256") {
		return Claims{}, ErrInvalidToken
	}
	key, err := v.key(ctx, header.KeyID)
	if err != nil {
		return Claims{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return Claims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if claims.Expires > 0 && time.Now().Unix() >= claims.Expires {
		return Claims{}, ErrInvalidToken
	}
	if v.Issuer != "" && claims.Issuer != v.Issuer {
		return Claims{}, ErrInvalidToken
	}
	if v.Audience != "" && !contains(claims.Audience, v.Audience) {
		return Claims{}, ErrInvalidToken
	}
	if strings.TrimSpace(claims.TenantID) == "" || strings.TrimSpace(claims.Subject) == "" {
		return Claims{}, ErrInvalidToken
	}
	claims.Role = NormalizeRole(claims.Role)
	return claims, nil
}

func (v *OIDCVerifier) key(ctx context.Context, keyID string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if key, ok := v.keys[keyID]; ok && time.Since(v.fetchedAt) < 5*time.Minute {
		return key, nil
	}
	if err := v.refreshLocked(ctx); err != nil {
		return nil, err
	}
	if key, ok := v.keys[keyID]; ok {
		return key, nil
	}
	return nil, ErrInvalidToken
}

func (v *OIDCVerifier) refreshLocked(ctx context.Context) error {
	jwksURL := v.JWKSURL
	if jwksURL == "" {
		discoveryURL := strings.TrimRight(v.Issuer, "/") + "/.well-known/openid-configuration"
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
		if err != nil {
			return err
		}
		response, err := v.Client.Do(request)
		if err != nil {
			return fmt.Errorf("oidc discovery: %w", err)
		}
		defer response.Body.Close()
		var discovery struct {
			JWKSURI string `json:"jwks_uri"`
		}
		if err := json.NewDecoder(response.Body).Decode(&discovery); err != nil {
			return err
		}
		jwksURL = discovery.JWKSURI
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return err
	}
	response, err := v.Client.Do(request)
	if err != nil {
		return fmt.Errorf("oidc jwks: %w", err)
	}
	defer response.Body.Close()
	var document struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		return err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, entry := range document.Keys {
		if !strings.EqualFold(entry.Kty, "RSA") || entry.N == "" || entry.E == "" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(entry.N)
		if err != nil {
			continue
		}
		exponent, err := base64.RawURLEncoding.DecodeString(entry.E)
		if err != nil {
			continue
		}
		keys[entry.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: int(new(big.Int).SetBytes(exponent).Int64())}
	}
	if len(keys) == 0 {
		return errors.New("oidc jwks contains no RSA keys")
	}
	v.keys = keys
	v.fetchedAt = time.Now()
	return nil
}

func contains(values Audience, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
