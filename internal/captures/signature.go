package captures

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"sync"
)

var (
	keyOnce sync.Once
	keyRing map[string]ed25519.PublicKey
)

// loadKeys reads GEO_CAPTURE_AGENT_KEYS, a JSON map of keyId to base64 public key.
func loadKeys() map[string]ed25519.PublicKey {
	keyOnce.Do(func() {
		keyRing = map[string]ed25519.PublicKey{}
		raw := os.Getenv("GEO_CAPTURE_AGENT_KEYS")
		if raw == "" {
			return
		}
		var decoded map[string]string
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			return
		}
		for id, value := range decoded {
			key, err := base64.StdEncoding.DecodeString(value)
			if err != nil || len(key) != ed25519.PublicKeySize {
				continue
			}
			keyRing[id] = ed25519.PublicKey(key)
		}
	})
	return keyRing
}

// VerifySignature checks the agent signature over the raw captures array.
// It returns false without error when the agent supplied no signature.
func VerifySignature(agent Agent, rawCaptures []byte) (bool, error) {
	if agent.Signature == "" || agent.PublicKeyID == "" {
		return false, nil
	}
	publicKey, ok := loadKeys()[agent.PublicKeyID]
	if !ok {
		return false, errors.New("unknown public key id")
	}
	signature, err := base64.StdEncoding.DecodeString(agent.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false, errors.New("signature is malformed")
	}
	sum := sha256.Sum256(rawCaptures)
	return ed25519.Verify(publicKey, sum[:], signature), nil
}

// RequiresSignature reports whether the method mandates a verified signature.
func RequiresSignature(method string) bool {
	return method == "partner_api" || method == "browser_extension"
}
