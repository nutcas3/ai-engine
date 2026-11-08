// Package auth provides OIDC authentication for /api/*: Bearer JWTs are
// validated against the issuer's JWKS (signature, exp, iss, aud, and an
// optional tenant claim for authz). When OIDC_ISSUER is unset the middleware
// is a passthrough — the dev path.
package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

type tenantKey struct{}

// TenantFrom returns the JWT's tenant claim ("tid" or "tenant").
func TenantFrom(ctx context.Context) string {
	if t, ok := ctx.Value(tenantKey{}).(string); ok {
		return t
	}
	return ""
}

// Middleware validates Bearer JWTs against a JWKS endpoint.
type Middleware struct {
	issuer, audience, jwksURL string
	mu     sync.Mutex
	keys   map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// New returns a Middleware when OIDC_ISSUER is configured, else nil (dev
// passthrough). JWKS resolves at <issuer>/.well-known/openid-configuration.
func New(issuer, audience string) *Middleware {
	if issuer == "" {
		return nil
	}
	return &Middleware{issuer: issuer, audience: audience,
		jwksURL: strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"}
}

// Require wraps a mux: mutating /api/* requests need a valid JWT; GET/health
// and pages stay open (dashboard read path). Tenant claim lands in context.
func (m *Middleware) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		claims, err := m.verify(r.Context(), strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
			return
		}
		tid, _ := claims["tid"].(string)
		if tid == "" {
			tid, _ = claims["tenant"].(string)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tenantKey{}, tid)))
	})
}

func (m *Middleware) verify(ctx context.Context, tok string) (map[string]any, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed jwt")
	}
	var hdr struct {
		Alg string `json:"alg"`; Kid string `json:"kid"`
	}
	if err := json.Unmarshal(b64(parts[0]), &hdr); err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(b64(parts[1]), &claims); err != nil {
		return nil, err
	}
	key, err := m.key(ctx, hdr.Kid)
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, h[:], sig); err != nil {
		return nil, err
	}
	if exp, ok := claims["exp"].(float64); ok && time.Now().Unix() > int64(exp) {
		return nil, fmt.Errorf("token expired")
	}
	if iss, _ := claims["iss"].(string); iss != m.issuer {
		return nil, fmt.Errorf("wrong issuer")
	}
	if m.audience != "" {
		if aud, _ := claims["aud"].(string); aud != m.audience {
			if arr, _ := claims["aud"].([]any); !contains(arr, m.audience) {
				return nil, fmt.Errorf("wrong audience")
			}
		}
	}
	return claims, nil
}

func (m *Middleware) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys == nil || time.Since(m.fetchedAt) > 10*time.Minute {
		if err := m.refresh(ctx); err != nil {
			return nil, err
		}
	}
	k, ok := m.keys[kid]
	if !ok && len(m.keys) == 1 {
		for _, v := range m.keys { return v, nil }
	}
	if !ok {
		return nil, fmt.Errorf("unknown kid %q", kid)
	}
	return k, nil
}

func (m *Middleware) refresh(ctx context.Context) error {
	jwksURL := m.jwksURL
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	doc, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// Resolve jwks_uri from discovery doc if this was the well-known URL.
	var disco struct{ JWKSURI string `json:"jwks_uri"` }
	if err := json.Unmarshal(doc, &disco); err == nil && disco.JWKSURI != "" {
		req, _ = http.NewRequestWithContext(ctx, http.MethodGet, disco.JWKSURI, nil)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		doc, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	var set struct {
		Keys []struct {
			Kid, Kty, N, E, X5c string
			X5cList []string `json:"x5c"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(doc, &set); err != nil {
		return err
	}
	m.keys = map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		switch {
		case len(k.X5cList) > 0:
			der, _ := base64.StdEncoding.DecodeString(k.X5cList[0])
			cert, err := x509.ParseCertificate(der)
			if err == nil {
				if pk, ok := cert.PublicKey.(*rsa.PublicKey); ok {
					m.keys[k.Kid] = pk
				}
			}
		case k.N != "" && k.E != "":
			nb, _ := base64.RawURLEncoding.DecodeString(k.N)
			eb, _ := base64.RawURLEncoding.DecodeString(k.E)
			e := 0
			for _, b := range eb {
				e = e<<8 | int(b)
			}
			m.keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
		}
	}
	m.fetchedAt = time.Now()
	return nil
}

func b64(s string) []byte {
	b, _ := base64.RawURLEncoding.DecodeString(s)
	return b
}

func contains(arr []any, v string) bool {
	for _, a := range arr {
		if s, _ := a.(string); s == v {
			return true
		}
	}
	return false
}
