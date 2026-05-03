package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/config"
)

// KiroHandlers holds dependencies for Kiro AI translation endpoints.
type KiroHandlers struct {
	Config *config.Config
	client *http.Client
}

// NewKiroHandlers creates a new KiroHandlers instance.
func NewKiroHandlers(cfg *config.Config) *KiroHandlers {
	return &KiroHandlers{
		Config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// englishToLuaRequest is the request body for English → Lua translation.
type englishToLuaRequest struct {
	Description string `json:"description"`
}

// englishToLuaResponse is the response body for English → Lua translation.
type englishToLuaResponse struct {
	LuaCode string `json:"lua_code"`
}

// luaToEnglishRequest is the request body for Lua → English translation.
type luaToEnglishRequest struct {
	LuaCode string `json:"lua_code"`
}

// luaToEnglishResponse is the response body for Lua → English translation.
type luaToEnglishResponse struct {
	Description string `json:"description"`
}

const englishToLuaSystemPrompt = `You are an expert at writing Lua email filtering rules. You translate plain English descriptions of email rules into Lua code.

The Lua rules have access to these globals and helpers:

- email.sender_address (string) — the sender's email address
- email.sender_name (string) — the sender's display name
- email.subject (string) — the email subject line
- email.date (table) — the email date
- email.has_attachments (boolean) — whether the email has attachments
- email.flags (table) — IMAP flags on the message

Helper functions:
- skip() — return this to skip the email (rule doesn't apply)
- keep(reason) — return this to keep the email in the inbox
- move(folder, reason) — return this to move the email to a folder
- archive(reason) — return this to archive the email
- delete_msg(reason) — return this to delete the email
- flag(name, reason) — return this to flag the email
- older_than_hours(n) — returns true if the email is older than n hours
- contains_any(str, patterns) — returns true if str contains any of the patterns
- has_ics() — returns true if the email has a .ics calendar attachment

Rules should:
1. Start with a comment block describing the rule
2. Extract relevant email fields into local variables
3. Check conditions and return skip() if the rule doesn't apply
4. Return an action (move, archive, delete_msg, keep, flag) with a reason string

Example rule:
` + "```lua" + `
-- Rule: Amazon
-- Move Amazon order/shipping emails older than 24 hours to @Amazon.

local sender = email.sender_address:lower()
local subject = email.subject:lower()

local domains = { "amazon.com", "marketplace.amazon.com" }
local keywords = { "your order", "has shipped", "delivered" }

local sender_match = false
for _, domain in ipairs(domains) do
    if sender:find(domain, 1, true) then
        sender_match = true
        break
    end
end

if not sender_match then return skip() end
if not contains_any(subject, keywords) then return skip() end
if not older_than_hours(24) then
    return keep("Amazon order email, keeping until 24h old")
end

return move("@Amazon", "Amazon order/shipping email filed")
` + "```" + `

Respond with ONLY the Lua code. No markdown fences, no explanation, just the raw Lua code.`

const luaToEnglishSystemPrompt = `You are an expert at reading Lua email filtering rules and explaining them in plain English.

Given a Lua email rule, describe what it does in clear, concise English. Focus on:
1. What emails the rule matches (sender, subject, conditions)
2. What action it takes (move, archive, delete, keep, flag)
3. Any timing conditions (e.g., "older than 24 hours")

Be concise but complete. Write a single paragraph or a few short sentences. Do not include any code in your response.`

// anthropicRequest is the request body for the Anthropic Messages API.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicMessage is a single message in the Anthropic API.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the response from the Anthropic Messages API.
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// callKiroAPI sends a prompt to the Kiro/Anthropic API and returns the response text.
func (h *KiroHandlers) callKiroAPI(systemPrompt, userMessage string) (string, error) {
	if h.Config.KiroAPIKey == "" {
		return "", fmt.Errorf("KIRO_API_KEY is not configured")
	}

	reqBody := anthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: userMessage},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", h.Config.KiroAPIURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", h.Config.KiroAPIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Error().
			Int("status", resp.StatusCode).
			Str("body", string(body)).
			Msg("Kiro API returned non-200 status")
		return "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("empty response from API")
	}

	return apiResp.Content[0].Text, nil
}

// TranslateEnglishToLua handles POST /api/kiro/translate/english-to-lua
func (h *KiroHandlers) TranslateEnglishToLua(c *fiber.Ctx) error {
	var req englishToLuaRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.Description == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "description is required"})
	}

	log.Info().Str("description", req.Description).Msg("translating English to Lua")

	luaCode, err := h.callKiroAPI(englishToLuaSystemPrompt, req.Description)
	if err != nil {
		log.Error().Err(err).Msg("failed to translate English to Lua")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "translation failed: " + err.Error()})
	}

	return c.JSON(englishToLuaResponse{LuaCode: luaCode})
}

// TranslateLuaToEnglish handles POST /api/kiro/translate/lua-to-english
func (h *KiroHandlers) TranslateLuaToEnglish(c *fiber.Ctx) error {
	var req luaToEnglishRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.LuaCode == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "lua_code is required"})
	}

	log.Info().Msg("translating Lua to English")

	description, err := h.callKiroAPI(luaToEnglishSystemPrompt, req.LuaCode)
	if err != nil {
		log.Error().Err(err).Msg("failed to translate Lua to English")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "translation failed: " + err.Error()})
	}

	return c.JSON(luaToEnglishResponse{Description: description})
}
