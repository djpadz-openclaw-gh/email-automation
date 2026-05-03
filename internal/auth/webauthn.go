package auth

import (
	"encoding/base64"
	"encoding/json"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// WebAuthnUser implements the webauthn.User interface.
type WebAuthnUser struct {
	ID          int64
	Username    string
	Credentials []webauthn.Credential
}

// WebAuthnUserID returns the user's ID as bytes.
func (u *WebAuthnUser) WebAuthnID() []byte {
	// Use big-endian encoding for user ID (standard for WebAuthn)
	// This ensures consistent encoding across registration and authentication
	buf := make([]byte, 8)
	buf[0] = byte(u.ID >> 56)
	buf[1] = byte(u.ID >> 48)
	buf[2] = byte(u.ID >> 40)
	buf[3] = byte(u.ID >> 32)
	buf[4] = byte(u.ID >> 24)
	buf[5] = byte(u.ID >> 16)
	buf[6] = byte(u.ID >> 8)
	buf[7] = byte(u.ID)
	return buf
}

// WebAuthnName returns the user's username.
func (u *WebAuthnUser) WebAuthnName() string {
	return u.Username
}

// WebAuthnDisplayName returns the user's display name.
func (u *WebAuthnUser) WebAuthnDisplayName() string {
	return u.Username
}

// WebAuthnCredentials returns the user's registered credentials.
func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

// WebAuthnIcon returns an empty string (deprecated in WebAuthn spec).
func (u *WebAuthnUser) WebAuthnIcon() string {
	return ""
}

// PasskeyRecord represents a stored passkey in the database.
type PasskeyRecord struct {
	ID           int64    `json:"id"`
	UserID       int64    `json:"user_id"`
	CredentialID string   `json:"credential_id"` // base64
	PublicKey    string   `json:"public_key"`     // base64
	SignCount    uint32   `json:"sign_count"`
	Transports   []string `json:"transports"`
	Name         string   `json:"name"`
	CreatedAt    string   `json:"created_at"`
	LastUsedAt   *string  `json:"last_used_at,omitempty"`
}

// ToWebAuthnCredential converts a PasskeyRecord to a webauthn.Credential.
func (p *PasskeyRecord) ToWebAuthnCredential() (webauthn.Credential, error) {
	credID, err := base64.RawURLEncoding.DecodeString(p.CredentialID)
	if err != nil {
		// Try standard encoding
		credID, err = base64.StdEncoding.DecodeString(p.CredentialID)
		if err != nil {
			return webauthn.Credential{}, err
		}
	}

	pubKey, err := base64.RawURLEncoding.DecodeString(p.PublicKey)
	if err != nil {
		pubKey, err = base64.StdEncoding.DecodeString(p.PublicKey)
		if err != nil {
			return webauthn.Credential{}, err
		}
	}

	var transports []protocol.AuthenticatorTransport
	for _, t := range p.Transports {
		transports = append(transports, protocol.AuthenticatorTransport(t))
	}

	return webauthn.Credential{
		ID:              credID,
		PublicKey:       pubKey,
		AttestationType: "none",
		Transport:       transports,
		Authenticator: webauthn.Authenticator{
			SignCount: p.SignCount,
		},
	}, nil
}

// TransportsJSON converts transports slice to JSON for DB storage.
func TransportsJSON(transports []protocol.AuthenticatorTransport) string {
	strs := make([]string, len(transports))
	for i, t := range transports {
		strs[i] = string(t)
	}
	b, _ := json.Marshal(strs)
	return string(b)
}
