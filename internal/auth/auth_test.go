package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func signHS256(t *testing.T, claims Claims, secret string) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	encode := func(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }
	signingInput := encode(header) + "." + encode(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + encode(mac.Sum(nil))
}

func TestParseHS256AcceptsValidToken(t *testing.T) {
	token := signHS256(t, Claims{Subject: "user-1", TenantID: "tenant-1", Role: "Analyst", Expires: time.Now().Add(time.Hour).Unix()}, "secret")
	claims, err := ParseHS256(token, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if claims.TenantID != "tenant-1" || claims.Role != "analyst" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestParseHS256RejectsTamperingAndExpiry(t *testing.T) {
	token := signHS256(t, Claims{Subject: "user-1", TenantID: "tenant-1", Role: "owner"}, "secret")
	if _, err := ParseHS256(token, "other-secret"); err == nil {
		t.Fatal("signature mismatch must be rejected")
	}
	if _, err := ParseHS256(token+".x", "secret"); err == nil {
		t.Fatal("malformed token must be rejected")
	}
	expired := signHS256(t, Claims{Subject: "user-1", TenantID: "tenant-1", Role: "owner", Expires: time.Now().Add(-time.Minute).Unix()}, "secret")
	if _, err := ParseHS256(expired, "secret"); err == nil {
		t.Fatal("expired token must be rejected")
	}
}

func TestCanFollowsP0Matrix(t *testing.T) {
	cases := []struct {
		role   string
		action string
		want   bool
	}{
		{"owner", "manage_members", true},
		{"analyst", "run_audit", true},
		{"analyst", "manage_members", false},
		{"editor", "edit_task", true},
		{"viewer", "edit_task", false},
		{"approver", "approve_task", true},
		{"editor", "approve_task", false},
		{"viewer", "read", true},
	}
	for _, test := range cases {
		if got := Can(test.role, test.action); got != test.want {
			t.Fatalf("Can(%s,%s) = %v, want %v", test.role, test.action, got, test.want)
		}
	}
}

func TestParseHS256RejectsEmptySecret(t *testing.T) {
	token := signHS256(t, Claims{Subject: "u", TenantID: "t"}, "")
	if _, err := ParseHS256(token, ""); err == nil {
		t.Fatal("empty secret must be rejected")
	}
}
