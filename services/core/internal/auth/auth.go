// SPDX-License-Identifier: AGPL-3.0-only

// Package auth validates user and device identity independently of HTTP handlers.
package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

var ErrUnauthenticated = errors.New("authentication failed")
var ErrDeviceRevoked = errors.New("device is revoked")

type Principal struct {
	TenantID  string
	ActorID   string
	Roles     []string
	OutletIDs []string
	Kind      string
	DeviceID  string
}

func (p Principal) HasRole(role string) bool {
	for _, value := range p.Roles {
		if value == role {
			return true
		}
	}
	return false
}

// IsPlatformAdmin is deliberately separate from an organization owner. It is
// used only by FeastCloud-operated control-plane routes, never as a shortcut
// around a restaurant tenant's normal authorization.
func (p Principal) IsPlatformAdmin() bool { return p.HasRole("platform_admin") }
func (p Principal) AllowsOutlet(outletID string) bool {
	if outletID == "" {
		return p.HasRole("owner") || p.HasRole("manager")
	}
	if p.HasRole("owner") {
		return true
	}
	for _, value := range p.OutletIDs {
		if value == "*" || value == outletID {
			return true
		}
	}
	return false
}

type Authenticator interface {
	Authenticate(context.Context, *http.Request) (Principal, error)
}

// DemoAuthenticator is intentionally explicit and must not be used by production mode.
type DemoAuthenticator struct{}

func (DemoAuthenticator) Authenticate(_ context.Context, request *http.Request) (Principal, error) {
	tenant := strings.TrimSpace(request.Header.Get("X-FeastCloud-Tenant-ID"))
	actor := strings.TrimSpace(request.Header.Get("X-FeastCloud-Actor-ID"))
	if tenant == "" || actor == "" {
		return Principal{}, ErrUnauthenticated
	}
	kind := "user"
	roles := []string{"manager"}
	// Demo mode represents an organization-wide manager explicitly. Production
	// OIDC managers remain constrained by their signed outlets claim.
	outlets := []string{"*"}
	deviceID := ""
	if request.Header.Get("X-FeastCloud-Platform-Admin") == "true" && actor == "platform-admin" {
		roles = []string{"platform_admin"}
		outlets = []string{}
	}
	if strings.HasPrefix(actor, "edge:") {
		kind = "device"
		roles = []string{"edge"}
		outlets = []string{"*"}
		deviceID = strings.TrimPrefix(actor, "edge:")
	}
	return Principal{TenantID: tenant, ActorID: actor, Roles: roles, OutletIDs: outlets, Kind: kind, DeviceID: deviceID}, nil
}

type OIDCAuthenticator struct {
	issuer, audience string
	key              *rsa.PublicKey
	now              func() time.Time
	clockSkew        time.Duration
}

func NewOIDCAuthenticator(issuer, audience, publicKeyFile string) (*OIDCAuthenticator, error) {
	raw, err := os.ReadFile(publicKeyFile)
	if err != nil {
		return nil, fmt.Errorf("oidc: read public key: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("oidc: public key is not PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		if certificate, certErr := x509.ParseCertificate(block.Bytes); certErr == nil {
			parsed = certificate.PublicKey
		} else {
			return nil, fmt.Errorf("oidc: parse public key: %w", err)
		}
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("oidc: public key must be RSA")
	}
	if issuer == "" || audience == "" {
		return nil, errors.New("oidc: issuer and audience are required")
	}
	return &OIDCAuthenticator{issuer: strings.TrimRight(issuer, "/"), audience: audience, key: key, now: time.Now, clockSkew: 60 * time.Second}, nil
}

type oidcClaims struct {
	Issuer      string   `json:"iss"`
	Subject     string   `json:"sub"`
	Audience    any      `json:"aud"`
	Expires     int64    `json:"exp"`
	NotBefore   int64    `json:"nbf"`
	TenantID    string   `json:"tenant_id"`
	Roles       []string `json:"roles"`
	Outlets     []string `json:"outlets"`
	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

func (authenticator *OIDCAuthenticator) Authenticate(_ context.Context, request *http.Request) (Principal, error) {
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return Principal{}, ErrUnauthenticated
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Principal{}, ErrUnauthenticated
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if json.Unmarshal(headerRaw, &header) != nil || header.Algorithm != "RS256" {
		return Principal{}, ErrUnauthenticated
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(authenticator.key, crypto.SHA256, digest[:], signature) != nil {
		return Principal{}, ErrUnauthenticated
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	var claims oidcClaims
	if json.Unmarshal(payload, &claims) != nil {
		return Principal{}, ErrUnauthenticated
	}
	now := authenticator.now().UTC()
	if claims.Issuer != authenticator.issuer || claims.Subject == "" || claims.TenantID == "" || !audienceContains(claims.Audience, authenticator.audience) || now.After(time.Unix(claims.Expires, 0).Add(authenticator.clockSkew)) || (claims.NotBefore > 0 && now.Add(authenticator.clockSkew).Before(time.Unix(claims.NotBefore, 0))) {
		return Principal{}, ErrUnauthenticated
	}
	roles := append(append([]string(nil), claims.Roles...), claims.RealmAccess.Roles...)
	return Principal{TenantID: claims.TenantID, ActorID: claims.Subject, Roles: unique(roles), OutletIDs: unique(claims.Outlets), Kind: "user"}, nil
}
func audienceContains(raw any, want string) bool {
	switch value := raw.(type) {
	case string:
		return value == want
	case []any:
		for _, item := range value {
			if text, ok := item.(string); ok && text == want {
				return true
			}
		}
	}
	return false
}
func unique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

type Device struct{ TenantID, OutletID, EdgeID, DeviceID, Fingerprint, Status string }
type DeviceRegistry interface {
	DeviceByFingerprint(context.Context, string, string) (Device, error)
}
type CertificateAuthenticator struct{ registry DeviceRegistry }

func NewCertificateAuthenticator(registry DeviceRegistry) *CertificateAuthenticator {
	return &CertificateAuthenticator{registry: registry}
}
func (authenticator *CertificateAuthenticator) Authenticate(ctx context.Context, request *http.Request) (Principal, error) {
	if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
		return Principal{}, ErrUnauthenticated
	}
	tenantHint := strings.TrimSpace(request.Header.Get("X-FeastCloud-Tenant-ID"))
	if tenantHint == "" {
		return Principal{}, ErrUnauthenticated
	}
	fingerprint := sha256.Sum256(request.TLS.PeerCertificates[0].Raw)
	device, err := authenticator.registry.DeviceByFingerprint(ctx, tenantHint, fmt.Sprintf("%x", fingerprint[:]))
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	if device.Status != "active" {
		return Principal{}, ErrDeviceRevoked
	}
	return Principal{TenantID: device.TenantID, ActorID: "edge:" + device.EdgeID, Roles: []string{"edge"}, OutletIDs: []string{device.OutletID}, Kind: "device", DeviceID: device.DeviceID}, nil
}
