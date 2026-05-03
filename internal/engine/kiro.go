package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/djpadz/email-automation/internal/models"
)

// KiroClient handles API calls to the Kiro/Anthropic API for semantic evaluation.
type KiroClient struct {
	apiKey  string
	apiURL  string
	client  *http.Client
	cache   *kiroCache
}

// kiroCache provides thread-safe caching for Kiro API responses.
type kiroCache struct {
	mu      sync.RWMutex
	entries map[string]*kiroCacheEntry
	maxSize int
	ttl     time.Duration
}

type kiroCacheEntry struct {
	result    bool
	timestamp time.Time
}

// NewKiroClient creates a new KiroClient for semantic evaluation.
func NewKiroClient(apiKey, apiURL string) *KiroClient {
	if apiURL == "" {
		apiURL = "https://api.anthropic.com/v1/messages"
	}
	return &KiroClient{
		apiKey: apiKey,
		apiURL: apiURL,
		client: &http.Client{Timeout: 30 * time.Second},
		cache: &kiroCache{
			entries: make(map[string]*kiroCacheEntry),
			maxSize: 500,
			ttl:     10 * time.Minute,
		},
	}
}

// Enabled returns true if the Kiro client is configured with an API key.
func (k *KiroClient) Enabled() bool {
	return k.apiKey != ""
}

// cacheKey generates a cache key for a given operation and inputs.
func cacheKey(op string, emailID string, question string) string {
	return fmt.Sprintf("%s:%s:%s", op, emailID, question)
}

// getCached returns a cached result if available and not expired.
func (c *kiroCache) get(key string) (bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return false, false
	}
	if time.Since(entry.timestamp) > c.ttl {
		return false, false
	}
	return entry.result, true
}

// set stores a result in the cache.
func (c *kiroCache) set(key string, result bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest entries if cache is full
	if len(c.entries) >= c.maxSize {
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, v := range c.entries {
			if first || v.timestamp.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.timestamp
				first = false
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}

	c.entries[key] = &kiroCacheEntry{
		result:    result,
		timestamp: time.Now(),
	}
}

// kiroAPIRequest is the request body for the Anthropic Messages API.
type kiroAPIRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system"`
	Messages  []kiroAPIMessage `json:"messages"`
}

// kiroAPIMessage supports both text-only and multimodal content.
// Content is either a string (text-only) or []kiroContentBlock (multimodal).
type kiroAPIMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// kiroContentBlock represents a single content block in a multimodal message.
type kiroContentBlock struct {
	Type   string            `json:"type"`
	Text   string            `json:"text,omitempty"`
	Source *kiroImageSource  `json:"source,omitempty"`
}

// kiroImageSource holds the base64-encoded image data for the Anthropic vision API.
type kiroImageSource struct {
	Type      string `json:"type"`       // always "base64"
	MediaType string `json:"media_type"` // e.g. "image/jpeg"
	Data      string `json:"data"`       // base64-encoded image bytes
}

type kiroAPIResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// classify asks Kiro whether an email matches a semantic criteria.
// When the email has image attachments, they are sent as vision content blocks
// to the Anthropic API for visual analysis (e.g., detecting fake invoices in images).
func (k *KiroClient) classify(email *models.EmailContext, question string) (bool, error) {
	if !k.Enabled() {
		return false, fmt.Errorf("kiro API key not configured")
	}

	// Check cache (include image presence in cache key to differentiate)
	cacheTag := "classify"
	if len(email.ImageAttachments) > 0 {
		cacheTag = "classify_vision"
	}
	key := cacheKey(cacheTag, email.MessageID, question)
	if result, ok := k.cache.get(key); ok {
		log.Debug().Str("question", question).Bool("cached_result", result).Msg("kiro.classify cache hit")
		return result, nil
	}

	emailSummary := formatEmailForKiro(email)

	var systemPrompt string
	var result bool
	var err error

	if len(email.ImageAttachments) > 0 {
		// Vision-enabled classification: send images + text to the API
		systemPrompt = `You are an email classification assistant with vision capabilities. You will be given an email (with text and image attachments) and a yes/no question about it. Analyze BOTH the email text AND the attached images to answer the question. Look carefully at the visual content of images — text rendered in images, logos, layouts, and visual patterns are all relevant. Answer ONLY with "yes" or "no" — nothing else.`

		userText := fmt.Sprintf("Email:\n%s\n\nQuestion: %s", emailSummary, question)
		result, err = k.askYesNoWithImages(systemPrompt, userText, email.ImageAttachments)
	} else {
		// Text-only classification
		systemPrompt = `You are an email classification assistant. You will be given an email and a yes/no question about it. Answer ONLY with "yes" or "no" — nothing else.`

		userMessage := fmt.Sprintf("Email:\n%s\n\nQuestion: %s", emailSummary, question)
		result, err = k.askYesNo(systemPrompt, userMessage)
	}

	if err != nil {
		return false, err
	}

	// Cache the result
	k.cache.set(key, result)
	return result, nil
}

// isActionable asks Kiro whether an email requires action from the recipient.
func (k *KiroClient) isActionable(email *models.EmailContext) (bool, error) {
	if !k.Enabled() {
		return false, fmt.Errorf("kiro API key not configured")
	}

	// Check cache
	key := cacheKey("is_actionable", email.MessageID, "")
	if result, ok := k.cache.get(key); ok {
		log.Debug().Bool("cached_result", result).Msg("kiro.is_actionable cache hit")
		return result, nil
	}

	emailSummary := formatEmailForKiro(email)

	systemPrompt := `You are an email triage assistant. You determine whether an email requires action from the recipient (reply needed, task to complete, decision to make, etc.) versus being purely informational (newsletters, notifications, receipts, automated alerts). Answer ONLY with "yes" or "no" — nothing else.`

	userMessage := fmt.Sprintf("Email:\n%s\n\nDoes this email require action from the recipient?", emailSummary)

	result, err := k.askYesNo(systemPrompt, userMessage)
	if err != nil {
		return false, err
	}

	// Cache the result
	k.cache.set(key, result)
	return result, nil
}

// isFakeInvoice asks Kiro whether an email appears to be a fake or suspicious invoice.
// It uses both OCR text and actual image attachments (via vision) when available.
func (k *KiroClient) isFakeInvoice(email *models.EmailContext) (bool, error) {
	if !k.Enabled() {
		return false, fmt.Errorf("kiro API key not configured")
	}

	// Check cache
	cacheTag := "is_fake_invoice"
	if len(email.ImageAttachments) > 0 {
		cacheTag = "is_fake_invoice_vision"
	}
	key := cacheKey(cacheTag, email.MessageID, "")
	if result, ok := k.cache.get(key); ok {
		log.Debug().Bool("cached_result", result).Msg("kiro.is_fake_invoice cache hit")
		return result, nil
	}

	emailSummary := formatEmailForKiro(email)

	// Include OCR text if available
	if email.OCRText != "" {
		emailSummary += fmt.Sprintf("\nOCR text extracted from image attachments:\n%s\n", email.OCRText)
	}

	var systemPrompt string
	var result bool
	var err error

	if len(email.ImageAttachments) > 0 {
		systemPrompt = `You are a fraud detection assistant with vision capabilities, specializing in invoice and payment scams. Analyze the email text AND the attached images for signs of a fake or suspicious invoice. Pay special attention to:
- Images that look like invoices, receipts, or payment confirmations (especially McAfee, Geek Squad, Norton, PayPal)
- Text rendered in images that differs from the email body (a common scam tactic)
- Fake logos, distorted branding, or low-quality reproductions
- Phone numbers in images urging you to call
- Urgency pressure ("pay immediately", "account will be suspended")
- Mismatched sender domains vs claimed company
- Suspicious payment details or unusual amounts

Answer ONLY with "yes" (suspicious/fake) or "no" (appears legitimate) — nothing else.`

		userMessage := fmt.Sprintf("Email:\n%s\n\nDoes this appear to be a fake, fraudulent, or suspicious invoice?", emailSummary)
		result, err = k.askYesNoWithImages(systemPrompt, userMessage, email.ImageAttachments)
	} else {
		systemPrompt = `You are a fraud detection assistant specializing in invoice and payment scams. Analyze the email (and any OCR text from attached images) for signs of a fake or suspicious invoice. Look for:
- Unexpected invoices from unknown vendors
- Urgency pressure ("pay immediately", "account will be suspended")
- Mismatched sender domains vs claimed company
- Suspicious payment details or unusual amounts
- Poor grammar/formatting typical of scam emails
- Requests to change payment methods or bank details
- Invoices for services never ordered

Answer ONLY with "yes" (suspicious/fake) or "no" (appears legitimate) — nothing else.`

		userMessage := fmt.Sprintf("Email:\n%s\n\nDoes this appear to be a fake, fraudulent, or suspicious invoice?", emailSummary)
		result, err = k.askYesNo(systemPrompt, userMessage)
	}

	if err != nil {
		return false, err
	}

	// Cache the result
	k.cache.set(key, result)
	return result, nil
}

// askYesNoWithImages sends a multimodal prompt (text + images) to the Anthropic API
// and interprets the response as yes/no. Uses claude-sonnet for vision capability.
func (k *KiroClient) askYesNoWithImages(systemPrompt, userText string, images []models.ImageAttachment) (bool, error) {
	// Build content blocks: images first, then the text question
	var contentBlocks []kiroContentBlock

	for _, img := range images {
		contentBlocks = append(contentBlocks, kiroContentBlock{
			Type: "image",
			Source: &kiroImageSource{
				Type:      "base64",
				MediaType: img.MediaType,
				Data:      img.Data,
			},
		})
	}

	contentBlocks = append(contentBlocks, kiroContentBlock{
		Type: "text",
		Text: userText,
	})

	// Use claude-sonnet for vision — haiku doesn't support images well
	reqBody := kiroAPIRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 10,
		System:    systemPrompt,
		Messages: []kiroAPIMessage{
			{Role: "user", Content: contentBlocks},
		},
	}

	log.Info().
		Int("image_count", len(images)).
		Str("model", reqBody.Model).
		Msg("sending vision request to kiro API")

	return k.doYesNoRequest(reqBody)
}

// askYesNo sends a text-only prompt to the API and interprets the response as yes/no.
func (k *KiroClient) askYesNo(systemPrompt, userMessage string) (bool, error) {
	reqBody := kiroAPIRequest{
		Model:     "claude-haiku-4.5",
		MaxTokens: 10,
		System:    systemPrompt,
		Messages: []kiroAPIMessage{
			{Role: "user", Content: userMessage},
		},
	}

	return k.doYesNoRequest(reqBody)
}

// doYesNoRequest executes the API request and interprets the response as yes/no.
func (k *KiroClient) doYesNoRequest(reqBody kiroAPIRequest) (bool, error) {

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return false, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", k.apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", k.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := k.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("kiro API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle rate limiting
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		log.Warn().Str("retry_after", retryAfter).Msg("kiro API rate limited")
		return false, fmt.Errorf("kiro API rate limited (retry after: %s)", retryAfter)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("status", resp.StatusCode).Str("body", string(body)).Msg("kiro API error")
		return false, fmt.Errorf("kiro API returned status %d", resp.StatusCode)
	}

	var apiResp kiroAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return false, fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.Error != nil {
		return false, fmt.Errorf("kiro API error: %s", apiResp.Error.Message)
	}

	// Extract text response
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			answer := strings.TrimSpace(strings.ToLower(block.Text))
			return answer == "yes", nil
		}
	}

	return false, fmt.Errorf("no text content in kiro API response")
}

// formatEmailForKiro creates a text summary of an email for the Kiro API.
func formatEmailForKiro(email *models.EmailContext) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("From: %s <%s>\n", email.SenderName, email.SenderAddress))
	b.WriteString(fmt.Sprintf("Subject: %s\n", email.Subject))
	b.WriteString(fmt.Sprintf("Date: %s\n", email.Date.Format(time.RFC3339)))
	if len(email.Recipients) > 0 {
		b.WriteString(fmt.Sprintf("To: %s\n", strings.Join(email.Recipients, ", ")))
	}
	if email.HasAttachments {
		b.WriteString(fmt.Sprintf("Attachments: %s\n", strings.Join(email.AttachmentNames, ", ")))
	}
	if email.BodyPreview != "" {
		b.WriteString(fmt.Sprintf("\nBody preview:\n%s\n", email.BodyPreview))
	}
	return b.String()
}

// registerKiroNamespace adds the kiro table to the Lua environment.
// The kiro table provides semantic evaluation functions that call the Kiro API.
func registerKiroNamespace(L *lua.LState, kiroClient *KiroClient, email *models.EmailContext) {
	kiroTable := L.NewTable()

	// kiro.classify(email, question) -> boolean
	// Ask Kiro if the email matches a semantic criteria.
	kiroTable.RawSetString("classify", L.NewFunction(func(L *lua.LState) int {
		// First arg can be the email table (ignored, we use the context) or a question string
		var question string
		arg1 := L.Get(1)
		if arg1.Type() == lua.LTTable {
			// kiro.classify(email, question) — email table is arg1, question is arg2
			question = L.CheckString(2)
		} else {
			// kiro.classify(question) — shorthand, uses current email context
			question = L.CheckString(1)
		}

		if kiroClient == nil || !kiroClient.Enabled() {
			log.Warn().Msg("kiro.classify called but Kiro API is not configured")
			L.Push(lua.LBool(false))
			return 1
		}

		result, err := kiroClient.classify(email, question)
		if err != nil {
			log.Error().Err(err).Str("question", question).Msg("kiro.classify failed")
			L.Push(lua.LBool(false))
			return 1
		}

		L.Push(lua.LBool(result))
		return 1
	}))

	// kiro.is_actionable(email?) -> boolean
	// Ask Kiro if the email requires action from the recipient.
	kiroTable.RawSetString("is_actionable", L.NewFunction(func(L *lua.LState) int {
		if kiroClient == nil || !kiroClient.Enabled() {
			log.Warn().Msg("kiro.is_actionable called but Kiro API is not configured")
			L.Push(lua.LBool(false))
			return 1
		}

		result, err := kiroClient.isActionable(email)
		if err != nil {
			log.Error().Err(err).Msg("kiro.is_actionable failed")
			L.Push(lua.LBool(false))
			return 1
		}

		L.Push(lua.LBool(result))
		return 1
	}))

	// kiro.is_fake_invoice(email?) -> boolean
	// Ask Kiro if the email appears to be a fake/suspicious invoice.
	// Uses OCR text from image attachments when available.
	kiroTable.RawSetString("is_fake_invoice", L.NewFunction(func(L *lua.LState) int {
		if kiroClient == nil || !kiroClient.Enabled() {
			log.Warn().Msg("kiro.is_fake_invoice called but Kiro API is not configured")
			L.Push(lua.LBool(false))
			return 1
		}

		result, err := kiroClient.isFakeInvoice(email)
		if err != nil {
			log.Error().Err(err).Msg("kiro.is_fake_invoice failed")
			L.Push(lua.LBool(false))
			return 1
		}

		L.Push(lua.LBool(result))
		return 1
	}))

	L.SetGlobal("kiro", kiroTable)
}
