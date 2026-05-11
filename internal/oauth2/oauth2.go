// Package oauth2 provides OAuth2 authentication flows for Microsoft 365 and Gmail.
package oauth2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// TokenResponse represents the response from an OAuth2 token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token,omitempty"`
}

// Provider defines the interface for an OAuth2 provider.
type Provider interface {
	// Name returns the provider identifier (e.g., "microsoft365", "gmail").
	Name() string
	// AuthURL returns the URL to redirect the user to for authorization.
	AuthURL(state string) string
	// ExchangeCode exchanges an authorization code for tokens.
	ExchangeCode(ctx context.Context, code string) (*TokenResponse, error)
	// RefreshToken exchanges a refresh token for a new access token.
	RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error)
	// IMAPHost returns the IMAP server host for this provider.
	IMAPHost() string
	// IMAPPort returns the IMAP server port for this provider.
	IMAPPort() int
	// UserEmail extracts the user's email from the token response or via API call.
	UserEmail(ctx context.Context, accessToken string) (string, error)
}

// Microsoft365Provider implements OAuth2 for Microsoft 365.
type Microsoft365Provider struct {
	ClientID     string
	ClientSecret string
	TenantID     string
	RedirectURI  string
}

// NewMicrosoft365Provider creates a new Microsoft 365 OAuth2 provider.
func NewMicrosoft365Provider(clientID, clientSecret, tenantID, redirectURI string) *Microsoft365Provider {
	return &Microsoft365Provider{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TenantID:     tenantID,
		RedirectURI:  redirectURI,
	}
}

func (p *Microsoft365Provider) Name() string {
	return "microsoft365"
}

func (p *Microsoft365Provider) AuthURL(state string) string {
	tenant := p.TenantID
	if tenant == "" {
		tenant = "common"
	}
	params := url.Values{
		"client_id":     {p.ClientID},
		"response_type": {"code"},
		"redirect_uri":  {p.RedirectURI},
		"scope":         {"https://outlook.office365.com/IMAP.AccessAsUser.All offline_access openid email profile"},
		"state":         {state},
		"response_mode": {"query"},
		"prompt":        {"consent"},
	}
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize?%s", tenant, params.Encode())
}

func (p *Microsoft365Provider) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	tenant := p.TenantID
	if tenant == "" {
		tenant = "common"
	}
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenant)

	data := url.Values{
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"code":          {code},
		"redirect_uri":  {p.RedirectURI},
		"grant_type":    {"authorization_code"},
		"scope":         {"https://outlook.office365.com/IMAP.AccessAsUser.All offline_access openid email profile"},
	}

	return postTokenRequest(ctx, tokenURL, data)
}

func (p *Microsoft365Provider) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	tenant := p.TenantID
	if tenant == "" {
		tenant = "common"
	}
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenant)

	data := url.Values{
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
		"scope":         {"https://outlook.office365.com/IMAP.AccessAsUser.All offline_access openid email profile"},
	}

	return postTokenRequest(ctx, tokenURL, data)
}

func (p *Microsoft365Provider) IMAPHost() string {
	return "outlook.office365.com"
}

func (p *Microsoft365Provider) IMAPPort() int {
	return 993
}

func (p *Microsoft365Provider) UserEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://graph.microsoft.com/v1.0/me", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("user info request failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Mail                string `json:"mail"`
		UserPrincipalName   string `json:"userPrincipalName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode user info: %w", err)
	}

	email := result.Mail
	if email == "" {
		email = result.UserPrincipalName
	}
	return email, nil
}

// GmailProvider implements OAuth2 for Gmail.
type GmailProvider struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

// NewGmailProvider creates a new Gmail OAuth2 provider.
func NewGmailProvider(clientID, clientSecret, redirectURI string) *GmailProvider {
	return &GmailProvider{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
	}
}

func (p *GmailProvider) Name() string {
	return "gmail"
}

func (p *GmailProvider) AuthURL(state string) string {
	params := url.Values{
		"client_id":     {p.ClientID},
		"response_type": {"code"},
		"redirect_uri":  {p.RedirectURI},
		"scope":         {"https://mail.google.com/ openid email profile"},
		"state":         {state},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
	}
	return fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?%s", params.Encode())
}

func (p *GmailProvider) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	data := url.Values{
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"code":          {code},
		"redirect_uri":  {p.RedirectURI},
		"grant_type":    {"authorization_code"},
	}

	return postTokenRequest(ctx, "https://oauth2.googleapis.com/token", data)
}

func (p *GmailProvider) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	data := url.Values{
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	return postTokenRequest(ctx, "https://oauth2.googleapis.com/token", data)
}

func (p *GmailProvider) IMAPHost() string {
	return "imap.gmail.com"
}

func (p *GmailProvider) IMAPPort() int {
	return 993
}

func (p *GmailProvider) UserEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("user info request failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode user info: %w", err)
	}

	return result.Email, nil
}

// BuildXOAuth2String builds the XOAUTH2 authentication string for IMAP.
// Format: "user=<email>\x01auth=Bearer <token>\x01\x01"
func BuildXOAuth2String(email, accessToken string) string {
	return fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", email, accessToken)
}

// XOAuth2Client implements the sasl.Client interface for the XOAUTH2 mechanism.
// This is used by Gmail and Microsoft 365 for IMAP authentication.
type XOAuth2Client struct {
	Username string
	Token    string
}

// Start begins XOAUTH2 authentication.
func (c *XOAuth2Client) Start() (mech string, ir []byte, err error) {
	ir = []byte(BuildXOAuth2String(c.Username, c.Token))
	return "XOAUTH2", ir, nil
}

// Next handles server challenges (error responses).
func (c *XOAuth2Client) Next(challenge []byte) ([]byte, error) {
	// XOAUTH2 sends an empty response to error challenges
	return []byte{}, nil
}

// IsTokenExpired checks if a token expiry time has passed (with a 5-minute buffer).
func IsTokenExpired(expiry *time.Time) bool {
	if expiry == nil {
		return true
	}
	return time.Now().Add(5 * time.Minute).After(*expiry)
}

// postTokenRequest makes a POST request to an OAuth2 token endpoint.
func postTokenRequest(ctx context.Context, tokenURL string, data url.Values) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Error().
			Int("status", resp.StatusCode).
			Str("body", string(body)).
			Str("url", tokenURL).
			Msg("OAuth2 token request failed")
		return nil, fmt.Errorf("token request failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	return &tokenResp, nil
}
