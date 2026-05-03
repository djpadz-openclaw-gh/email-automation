package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
		client: &http.Client{Timeout: 60 * time.Second},
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
- email.body_preview (string) — first ~200 chars of the email body
- email.date (string) — the email date in RFC3339 format
- email.age_seconds (number) — how old the email is in seconds
- email.has_attachments (boolean) — whether the email has attachments
- email.attachment_names (table) — list of attachment filenames
- email.attachment_types (table) — list of attachment MIME types
- email.recipients (table) — list of recipient addresses
- email.headers (table) — email headers as key-value pairs
- email.folder (string) — current IMAP folder

Action functions (call one to set result):
- skip() — rule doesn't apply
- keep(reason) — keep in inbox, stop rule chain
- move(folder, reason) — move to a folder
- archive(reason) — archive the email
- delete(reason) — delete the email
- notify(message, reason) — send a notification
- move_after(folder, delay_secs, reason) — move after a delay
- delete_after(delay_secs, reason) — delete after a delay

Helper functions:
- contains(haystack, needle) — case-insensitive substring match
- contains_any(haystack, {needles}) — case-insensitive, matches any
- starts_with(text, prefix) — case-insensitive prefix match
- ends_with(text, suffix) — case-insensitive suffix match
- domain_of(email_addr) — extract domain from email address
- older_than(secs), older_than_hours(h), older_than_days(d) — age checks
- has_ics() — has calendar attachment
- has_attachment_type(mime) — has attachment of given MIME type
- is_reply() — subject starts with Re:/Fwd:/etc.
- now_hour() — current hour in UTC

Kiro AI functions (for SEMANTIC evaluation only):
- kiro.classify(email, question) — ask AI a yes/no question about the email
  VISION CAPABLE: When the email has image attachments, kiro.classify() automatically
  sends the actual images to the AI for visual analysis. It can see logos, layouts,
  text rendered in images, and visual patterns — not just OCR text.
- kiro.is_actionable(email) — ask AI if the email requires action
- kiro.is_fake_invoice(email) — ask AI if an invoice/payment email looks fraudulent (uses vision + OCR)

Additional email fields for images and OCR:
- email.ocr_text (string) — text extracted from image attachments via OCR (always available when images are present)
- email.has_images (boolean) — whether the email has image attachments
- Image attachments are automatically passed to kiro.classify() and kiro.is_fake_invoice() for vision analysis

IMPORTANT: kiro.classify() now has VISION capabilities. When an email has image attachments,
the actual images are sent to the AI alongside the email text. This means kiro.classify() can:
- See what an image looks like (logos, layouts, colors, formatting)
- Read text rendered in images (even if OCR missed it)
- Detect visual patterns (fake invoices, phishing screenshots, scam receipts)
- Analyze image quality and authenticity cues
This is MORE POWERFUL than just OCR text — it's full visual understanding.

IMPORTANT GUIDELINES FOR CHOOSING BETWEEN SIMPLE PATTERNS AND KIRO:

1. PREFER simple string matching for concrete, deterministic criteria:
   - Matching specific senders, domains, subjects → use contains(), domain_of(), etc.
   - "emails from John" → sender_address or sender_name matching
   - "emails about shipping" → contains(subject, "shipping")
   - "newsletters" → contains(sender, "newsletter") or domain matching

2. Use kiro.classify() ONLY when the request is inherently semantic/subjective:
   - "important emails" → kiro.classify(email, "Is this email important or urgent?")
   - "invoices from vendors" → kiro.classify(email, "Is this an invoice from a vendor?")
   - "spam that got through" → kiro.classify(email, "Does this look like spam?")
   - "emails that need a reply" → kiro.is_actionable(email)

3. Use kiro.classify() for IMAGE-BASED detection:
   When the user mentions "picture of", "image of", "image contains", "image looks like",
   "screenshot of", "image with text", "photo of", or any reference to visual content in
   attachments, this is a signal to use kiro.classify() — NOT filename/MIME type checking.
   kiro.classify() has VISION capabilities — when images are attached, it sends the actual
   images to the AI for visual analysis. It can see logos, layouts, text in images, and
   visual patterns. Ask it about the image content:
   - "image that looks like a McAfee invoice" → kiro.classify(email, "Does this email have an image that looks like a McAfee or Geek Squad invoice?")
   - "screenshot of a bank login page" → kiro.classify(email, "Does this email contain an image that appears to be a bank login page?")
   - "picture of a receipt" → kiro.classify(email, "Does this email have an image that looks like a receipt?")
   - "image with the words 'You owe'" → kiro.classify(email, "Does this email have an image containing the text 'You owe'?")
   You can also check email.has_images first as a fast pre-filter before calling kiro.classify().
   You can also check email.ocr_text directly with contains() for exact text matches in images.

4. Use kiro.is_actionable() for action/triage questions:
   - "actionable emails" → kiro.is_actionable(email)
   - "emails I need to respond to" → kiro.is_actionable(email)

5. Use kiro.is_fake_invoice() for fraud detection:
   - "suspicious invoices" → kiro.is_fake_invoice(email)
   - "fake payment requests" → kiro.is_fake_invoice(email)

Rules should:
1. Start with a comment block describing the rule
2. Use simple pattern matching first (fast, no API calls)
3. Only fall back to kiro.classify() when the criteria cannot be expressed as string matching
4. Return skip() if the rule doesn't apply
5. Return an action with a reason string

Example 1 - Simple pattern (NO kiro needed):
` + "```lua" + `
-- Rule: Amazon Orders
-- Move Amazon order/shipping emails older than 24 hours to @Amazon.

local sender = email.sender_address:lower()
local subject = email.subject:lower()

if not ends_with(sender, "amazon.com") then return skip() end
if not contains_any(subject, {"your order", "has shipped", "delivered"}) then return skip() end
if not older_than_hours(24) then
    return keep("Amazon order email, keeping until 24h old")
end

return move("@Amazon", "Amazon order/shipping email filed")
` + "```" + `

Example 2 - Semantic evaluation (kiro needed):
` + "```lua" + `
-- Rule: Vendor Invoices
-- Detect invoices from vendors and move to @Invoices.

if not kiro.classify(email, "Is this an invoice or payment request from a vendor or supplier?") then
    return skip()
end

return move("@Invoices", "Vendor invoice detected by AI")
` + "```" + `

Example 3 - Combined approach:
` + "```lua" + `
-- Rule: Actionable emails from team
-- Keep actionable emails from the team, archive the rest.

local sender = email.sender_address:lower()
if not ends_with(sender, "@mycompany.com") then return skip() end

if kiro.is_actionable(email) then
    return keep("Actionable email from team member")
end

return archive("Non-actionable team email")
` + "```" + `

Example 4 - Image-based detection (kiro + OCR):
` + "```lua" + `
-- Rule: Fake McAfee/Geek Squad Invoices
-- Flag emails with images that look like McAfee or Geek Squad invoices as junk.

if not email.has_images then return skip() end

if kiro.classify(email, "Does this email have an image containing McAfee or Geek Squad invoice or renewal text?") then
    return move("Junk", "Image contains McAfee/Geek Squad invoice text (likely scam)")
end

return skip()
` + "```" + `

Example 5 - Image OCR with direct text matching:
` + "```lua" + `
-- Rule: Bank Login Screenshots
-- Flag emails containing screenshots of bank login pages.

if not email.has_images then return skip() end

-- Quick check: does OCR text mention banking keywords?
local ocr = email.ocr_text:lower()
if not contains_any(ocr, {"login", "sign in", "password", "account"}) then return skip() end

-- Semantic check: does it actually look like a bank login page?
if kiro.classify(email, "Does this email contain an image that appears to be a bank or financial login page?") then
    return move("Junk", "Image appears to be a bank login page screenshot (phishing)")
end

return skip()
` + "```" + `

Example 6 - Image content with OCR text matching:
` + "```lua" + `
-- Rule: Receipt Images
-- Move emails with receipt images to @Receipts.

if not email.has_images then return skip() end

if kiro.classify(email, "Does this email have an image that looks like a purchase receipt or order confirmation?") then
    return move("@Receipts", "Image contains receipt detected by AI")
end

return skip()
` + "```" + `

Respond with ONLY the Lua code. No markdown fences, no explanation, just the raw Lua code.`

const luaToEnglishSystemPrompt = `You are an expert at reading Lua email filtering rules and explaining them in plain English.

Given a Lua email rule, describe what it does in clear, concise English. Focus on:
1. What emails the rule matches (sender, subject, conditions)
2. What action it takes (move, archive, delete, keep, flag, notify)
3. Any timing conditions (e.g., "older than 24 hours")
4. Any AI/semantic evaluation (kiro.classify or kiro.is_actionable calls)

Note: Rules may use kiro.classify(email, question) for AI-based semantic classification
or kiro.is_actionable(email) to determine if an email requires action. When describing
these, explain what semantic criteria the rule is checking.

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

// looksLikeLua checks whether text appears to be valid Lua code by inspecting
// the first non-empty line for common Lua tokens.
func looksLikeLua(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	// Lua code typically starts with one of these tokens.
	prefixes := []string{
		"--",       // comment
		"local ",   // local declaration
		"if ",      // conditional
		"function ", // function definition
		"for ",     // for loop
		"while ",   // while loop
		"repeat",   // repeat-until
		"return ",  // return statement
		"do",       // do block
	}

	lower := strings.ToLower(trimmed)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
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

	req, err := http.NewRequest("POST", h.Config.KiroAPIURL, bytes.NewReader(jsonBody))
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

	// Find the text block (skip thinking blocks)
	for _, block := range apiResp.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("no text content in API response")

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

	// If the response doesn't look like Lua code, return it as an error
	// so the frontend can show a banner instead of replacing the editor content.
	if !looksLikeLua(luaCode) {
		return c.JSON(fiber.Map{
			"error":    "The AI response was not valid Lua code",
			"message":  luaCode,
			"lua_code": "",
		})
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

// Proxy accepts an Anthropic-compatible request and proxies it to the configured Kiro API endpoint.
// It parses the upstream response and returns only the text content block, filtering out thinking blocks.
func (h *KiroHandlers) Proxy(c *fiber.Ctx) error {
	if h.Config.KiroAPIKey == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Kiro API key not configured",
		})
	}

	body := c.Body()
	if len(body) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "request body is required",
		})
	}

	req, err := http.NewRequestWithContext(c.Context(), http.MethodPost, h.Config.KiroAPIURL, bytes.NewReader(body))
	if err != nil {
		log.Error().Err(err).Msg("failed to create upstream request")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to create upstream request",
		})
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", h.Config.KiroAPIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := h.client.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("failed to proxy request to Kiro API")
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "failed to reach upstream API",
		})
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msg("failed to read upstream response")
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "failed to read upstream response",
		})
	}

	// For non-200 responses, pass through as-is
	if resp.StatusCode != http.StatusOK {
		c.Set("Content-Type", resp.Header.Get("Content-Type"))
		return c.Status(resp.StatusCode).Send(respBody)
	}

	// Parse the Anthropic response and extract only the text content block
	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		log.Error().Err(err).Str("body", string(respBody)).Msg("failed to parse upstream response")
		// Fall back to raw response if parsing fails
		c.Set("Content-Type", resp.Header.Get("Content-Type"))
		return c.Status(resp.StatusCode).Send(respBody)
	}

	if apiResp.Error != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": apiResp.Error.Message,
		})
	}

	// Find the text block, skipping thinking blocks
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			text := block.Text
			// If the response doesn't look like Lua, return an error field
			// so the frontend can show a banner instead of updating the editor.
			if !looksLikeLua(text) {
				return c.JSON(fiber.Map{
					"error":   "The AI response was not valid Lua code",
					"message": text,
				})
			}
			return c.JSON(fiber.Map{
				"content": []fiber.Map{
					{
						"type": "text",
						"text": text,
					},
				},
			})
		}
	}

	log.Error().Str("body", string(respBody)).Msg("no text content block in upstream response")
	return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
		"error": "no text content in API response",
	})
}
