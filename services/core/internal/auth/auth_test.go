// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOIDCAuthenticatorValidatesSignatureClaimsAndScope(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(t.TempDir(), "oidc-public.pem")
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	authenticator, err := NewOIDCAuthenticator("https://identity.example/realms/feastcloud", "feastcloud-core", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	authenticator.now = func() time.Time { return now }

	claims := map[string]any{
		"iss": "https://identity.example/realms/feastcloud", "sub": "user-1",
		"aud": []string{"account", "feastcloud-core"}, "exp": now.Add(time.Hour).Unix(),
		"tenant_id": "tenant-1", "outlets": []string{"outlet-1"},
		"realm_access": map[string]any{"roles": []string{"cashier"}},
	}
	request := httptest.NewRequest("GET", "/api/v1/orders", nil)
	request.Header.Set("Authorization", "Bearer "+signedToken(t, key, claims))
	principal, err := authenticator.Authenticate(context.Background(), request)
	if err != nil {
		t.Fatalf("authenticate valid token: %v", err)
	}
	if principal.TenantID != "tenant-1" || principal.ActorID != "user-1" || !principal.HasRole("cashier") || !principal.AllowsOutlet("outlet-1") || principal.AllowsOutlet("outlet-2") {
		t.Fatalf("unexpected principal: %#v", principal)
	}

	claims["exp"] = now.Add(-2 * time.Minute).Unix()
	request.Header.Set("Authorization", "Bearer "+signedToken(t, key, claims))
	if _, err := authenticator.Authenticate(context.Background(), request); err == nil {
		t.Fatal("expired token was accepted")
	}
	claims["exp"] = now.Add(time.Hour).Unix()
	claims["iss"] = "https://attacker.example"
	request.Header.Set("Authorization", "Bearer "+signedToken(t, key, claims))
	if _, err := authenticator.Authenticate(context.Background(), request); err == nil {
		t.Fatal("wrong issuer was accepted")
	}
}

func TestManagerHonorsSignedOutletScope(t *testing.T) {
	principal := Principal{
		TenantID:  "tenant-1",
		ActorID:   "manager-1",
		Roles:     []string{"manager"},
		OutletIDs: []string{"outlet-1"},
	}
	if !principal.AllowsOutlet("outlet-1") {
		t.Fatal("manager was denied their assigned outlet")
	}
	if principal.AllowsOutlet("outlet-2") {
		t.Fatal("manager was allowed an outlet missing from the signed scope")
	}
	if !(Principal{Roles: []string{"owner"}}).AllowsOutlet("outlet-2") {
		t.Fatal("organization owner should retain cross-outlet access")
	}
	if (Principal{Roles: []string{"cashier"}, OutletIDs: []string{"outlet-1"}}).AllowsOutlet("") {
		t.Fatal("outlet-scoped user was allowed an unscoped outlet query")
	}

	request := httptest.NewRequest("GET", "/api/v1/dashboard/daily", nil)
	request.Header.Set("X-FeastCloud-Tenant-ID", "tenant-1")
	request.Header.Set("X-FeastCloud-Actor-ID", "manager-dashboard")
	demoPrincipal, err := (DemoAuthenticator{}).Authenticate(context.Background(), request)
	if err != nil || !demoPrincipal.AllowsOutlet("outlet-1") || !demoPrincipal.AllowsOutlet("outlet-2") {
		t.Fatalf("demo manager should have explicit wildcard scope: principal=%#v error=%v", demoPrincipal, err)
	}
}

func TestCertificateAuthenticatorRejectsRevokedDevice(t *testing.T) {
	certificate := &x509.Certificate{Raw: []byte("edge-certificate")}
	fingerprint := sha256.Sum256(certificate.Raw)
	registry := staticRegistry{device: Device{TenantID: "tenant-1", OutletID: "outlet-1", EdgeID: "edge-1", DeviceID: "device-1", Fingerprint: base64.RawStdEncoding.EncodeToString(fingerprint[:]), Status: "revoked"}}
	authenticator := NewCertificateAuthenticator(registry)
	request := httptest.NewRequest("POST", "/api/v1/sync/operations", nil)
	request.Header.Set("X-FeastCloud-Tenant-ID", "tenant-1")
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{certificate}}
	if _, err := authenticator.Authenticate(context.Background(), request); err != ErrDeviceRevoked {
		t.Fatalf("revoked device error=%v want %v", err, ErrDeviceRevoked)
	}
	registry.device.Status = "active"
	authenticator = NewCertificateAuthenticator(registry)
	principal, err := authenticator.Authenticate(context.Background(), request)
	if err != nil || principal.DeviceID != "device-1" || !principal.AllowsOutlet("outlet-1") || principal.AllowsOutlet("outlet-2") {
		t.Fatalf("unexpected active device principal=%#v error=%v", principal, err)
	}
}

type staticRegistry struct{ device Device }

func (registry staticRegistry) DeviceByFingerprint(_ context.Context, _, _ string) (Device, error) {
	return registry.device, nil
}

func signedToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}
