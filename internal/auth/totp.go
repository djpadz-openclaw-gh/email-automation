package auth

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"

	"github.com/pquerna/otp/totp"
)

// TOTPGenerateResult holds the result of TOTP secret generation.
type TOTPGenerateResult struct {
	Secret  string `json:"secret"`
	URL     string `json:"url"`
	QRCode  string `json:"qr_code"` // base64 data URL
}

// GenerateTOTP creates a new TOTP secret and QR code for a user.
func GenerateTOTP(username string) (*TOTPGenerateResult, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Email Automation",
		AccountName: username,
	})
	if err != nil {
		return nil, fmt.Errorf("generate TOTP: %w", err)
	}

	// Generate QR code image
	img, err := key.Image(200, 200)
	if err != nil {
		return nil, fmt.Errorf("generate QR image: %w", err)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode QR PNG: %w", err)
	}

	qrDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	return &TOTPGenerateResult{
		Secret: key.Secret(),
		URL:    key.URL(),
		QRCode: qrDataURL,
	}, nil
}

// ValidateTOTP validates a TOTP token against a secret.
func ValidateTOTP(token, secret string) bool {
	return totp.Validate(token, secret)
}
