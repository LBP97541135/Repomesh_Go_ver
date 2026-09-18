package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"time"
)

func (c *Client) AppCapability(ctx context.Context, owner, name string) (Capability, error) {
	observation, err := c.ObserveAppInstallation(ctx, owner, name)
	if err != nil {
		return Capability{}, err
	}
	return observation.Capability(), nil
}

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	if len(data) > 64<<10 {
		return nil, &Error{Kind: "unavailable"}
	}
	block, remainder := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(remainder)) != 0 || len(block.Headers) != 0 {
		return nil, &Error{Kind: "unavailable"}
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, &Error{Kind: "unavailable"}
		}
		key = parsed
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, &Error{Kind: "unavailable"}
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, &Error{Kind: "unavailable"}
		}
	default:
		return nil, &Error{Kind: "unavailable"}
	}
	if key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, &Error{Kind: "unavailable"}
	}
	return key, nil
}

func (c *Client) appToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	if c.appJWT != "" && now.Add(30*time.Second).Before(c.appJWTExp) {
		return c.appJWT, nil
	}
	keyPEM, err := c.config.PrivateKey(ctx)
	if err != nil {
		return "", &Error{Kind: "unavailable"}
	}
	key, err := parsePrivateKey(keyPEM)
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(struct {
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
		Issuer    string `json:"iss"`
	}{IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(9 * time.Minute).Unix(), Issuer: c.config.ClientID})
	if err != nil {
		return "", &Error{Kind: "unavailable"}
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(encoded))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", &Error{Kind: "unavailable"}
	}
	token := encoded + "." + base64.RawURLEncoding.EncodeToString(signature)
	c.appJWT = token
	c.appJWTExp = now.Add(8 * time.Minute)
	return token, nil
}
