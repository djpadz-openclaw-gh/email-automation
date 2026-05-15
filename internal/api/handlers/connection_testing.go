package handlers

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/config"
	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/oauth2"
)

// ConnectionTestHandlers holds dependencies for connection test HTTP handlers.
type ConnectionTestHandlers struct {
	DB             *db.DB
	oauthProviders map[string]oauth2.Provider
}

// NewConnectionTestHandlers creates a new ConnectionTestHandlers with OAuth providers from config.
func NewConnectionTestHandlers(database *db.DB, cfg *config.Config) *ConnectionTestHandlers {
	h := &ConnectionTestHandlers{
		DB:             database,
		oauthProviders: make(map[string]oauth2.Provider),
	}

	baseURL := cfg.OAuth2RedirectBaseURL

	// Register Microsoft 365 provider
	if cfg.OAuth2Microsoft365ClientID != "" && cfg.OAuth2Microsoft365ClientSecret != "" {
		redirectURI := baseURL + "/api/oauth2/callback/microsoft365"
		h.oauthProviders["microsoft365"] = oauth2.NewMicrosoft365Provider(
			cfg.OAuth2Microsoft365ClientID,
			cfg.OAuth2Microsoft365ClientSecret,
			cfg.OAuth2Microsoft365TenantID,
			redirectURI,
		)
	}

	// Register Gmail provider
	if cfg.OAuth2GmailClientID != "" && cfg.OAuth2GmailClientSecret != "" {
		redirectURI := baseURL + "/api/oauth2/callback/gmail"
		h.oauthProviders["gmail"] = oauth2.NewGmailProvider(
			cfg.OAuth2GmailClientID,
			cfg.OAuth2GmailClientSecret,
			redirectURI,
		)
	}

	return h
}

// ConnectionTestResult is the response from a connection test.
type ConnectionTestResult struct {
	Status   string `json:"status"` // "connected" or "failed"
	Error    string `json:"error,omitempty"`
	TestedAt string `json:"tested_at"`
}

func (h *ConnectionTestHandlers) tenantID(c *fiber.Ctx) int64 {
	if tid, ok := c.Locals("tenant_id").(int64); ok {
		return tid
	}
	return 0
}

// TestConnection tests IMAP/OAuth connectivity for an account.
func (h *ConnectionTestHandlers) TestConnection(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid account ID"})
	}

	// Get account with credentials
	account, err := h.DB.GetAccount(c.Context(), h.tenantID(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "account not found"})
	}

	// Test connection with a 10-second timeout
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	var testErr error

	host := account.IMAPHost
	port := account.IMAPPort
	if port == 0 {
		port = 993
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	// Connect to IMAP server
	opts := &imapclient.Options{
		TLSConfig: &tls.Config{
			ServerName: host,
		},
	}

	var client *imapclient.Client

	if account.IMAPTLS {
		client, testErr = imapclient.DialTLS(addr, opts)
	} else {
		var conn net.Conn
		conn, testErr = net.DialTimeout("tcp", addr, 10*time.Second)
		if testErr == nil {
			client = imapclient.New(conn, opts)
		}
	}

	if testErr != nil {
		testErr = fmt.Errorf("connect to %s: %w", addr, testErr)
	}

	if testErr == nil {
		// Wait for greeting
		if err := client.WaitGreeting(); err != nil {
			testErr = fmt.Errorf("server greeting: %w", err)
		}
	}

	if testErr == nil {
		// Authenticate
		username := account.Username
		if username == "" {
			username = account.Email
		}

		if account.OAuthProvider != "" && account.OAuthToken != "" {
			// OAuth authentication - refresh token if expired
			token := account.OAuthToken
			if oauth2.IsTokenExpired(account.OAuthTokenExpiry) {
				if provider, ok := h.oauthProviders[account.OAuthProvider]; ok {
					tokenResp, refreshErr := provider.RefreshToken(ctx, account.OAuthRefreshToken)
					if refreshErr != nil {
						testErr = fmt.Errorf("refresh OAuth2 token: %w", refreshErr)
					} else {
						token = tokenResp.AccessToken
						// Update stored token
						expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
						refreshToken := tokenResp.RefreshToken
						if refreshToken == "" {
							refreshToken = account.OAuthRefreshToken
						}
						_ = h.DB.UpdateAccountOAuthTokens(ctx, account.ID, tokenResp.AccessToken, refreshToken, &expiry)
					}
				} else {
					testErr = fmt.Errorf("OAuth2 provider %s not configured", account.OAuthProvider)
				}
			}

			if testErr == nil {
				xoauth2Client := &oauth2.XOAuth2Client{
					Username: username,
					Token:    token,
				}
				if err := client.Authenticate(xoauth2Client); err != nil {
					testErr = fmt.Errorf("XOAUTH2 authentication failed: %w", err)
				}
			}
		} else {
			// Password authentication
			loginCmd := client.Login(username, account.Password)
			if err := loginCmd.Wait(); err != nil {
				testErr = fmt.Errorf("login failed: %w", err)
			}
		}
	}

	// Clean up connection
	if client != nil {
		logoutCmd := client.Logout()
		_ = logoutCmd.Wait()
		client.Close()
	}

	// Build result
	now := time.Now().UTC()
	result := ConnectionTestResult{
		TestedAt: now.Format(time.RFC3339),
	}

	if testErr != nil {
		result.Status = "failed"
		result.Error = testErr.Error()
		log.Warn().Err(testErr).Int64("account_id", id).Msg("connection test failed")
	} else {
		result.Status = "connected"
		log.Info().Int64("account_id", id).Msg("connection test successful")
	}

	// Store result in database (best-effort, don't fail the request)
	errMsg := ""
	if testErr != nil {
		errMsg = testErr.Error()
	}
	if dbErr := h.DB.UpdateConnectionTestResult(ctx, id, result.Status, errMsg); dbErr != nil {
		log.Error().Err(dbErr).Int64("account_id", id).Msg("failed to store connection test result")
	}

	return c.JSON(result)
}
