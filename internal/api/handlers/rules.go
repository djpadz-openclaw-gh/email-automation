package handlers

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/engine"
	"github.com/djpadz/email-automation/internal/imapactions"
	"github.com/djpadz/email-automation/internal/models"
)

// kiroCallPattern matches any kiro.* function call in Lua code.
var kiroCallPattern = regexp.MustCompile(`\bkiro\.\w+\s*\(`)

// RuleHandlers holds dependencies for rule HTTP handlers.
type RuleHandlers struct {
	DB     *db.DB
	Engine *engine.Engine

	// activeOps tracks cancel functions for in-progress dry-run/execute operations.
	// Key: "tenant:<tenantID>:rule:<ruleID>" → cancel func
	activeOps sync.Map
}

func (h *RuleHandlers) tenantID(c *fiber.Ctx) int64 {
	if tid, ok := c.Locals("tenant_id").(int64); ok {
		return tid
	}
	return 0
}

// ListRules returns all rules for the tenant.
func (h *RuleHandlers) ListRules(c *fiber.Ctx) error {
	rules, err := h.DB.ListRules(c.Context(), h.tenantID(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if rules == nil {
		rules = []models.Rule{}
	}
	return c.JSON(rules)
}

// GetRule returns a single rule by ID.
func (h *RuleHandlers) GetRule(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid rule ID"})
	}

	rule, err := h.DB.GetRule(c.Context(), h.tenantID(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "rule not found"})
	}
	return c.JSON(rule)
}

// detectUsesAI returns true if the Lua code contains any kiro.* function calls.
func detectUsesAI(luaCode string) bool {
	return kiroCallPattern.MatchString(luaCode)
}

// CreateRule creates a new rule.
func (h *RuleHandlers) CreateRule(c *fiber.Ctx) error {
	var rule models.Rule
	if err := c.BodyParser(&rule); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	rule.TenantID = h.tenantID(c)
	if rule.Source == "" {
		rule.Source = "manual"
	}
	if rule.Source == "manual" {
		rule.Approved = true
	}

	// Auto-detect AI usage from Lua code
	rule.UsesAI = detectUsesAI(rule.LuaCode)

	// Validate Lua syntax
	if err := h.Engine.ValidateLua(rule.LuaCode); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid Lua code",
			"details": err.Error(),
		})
	}

	if err := h.DB.CreateRule(c.Context(), &rule); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(rule)
}

// UpdateRule updates an existing rule.
func (h *RuleHandlers) UpdateRule(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid rule ID"})
	}

	var rule models.Rule
	if err := c.BodyParser(&rule); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	rule.ID = id
	rule.TenantID = h.tenantID(c)

	// Auto-detect AI usage from Lua code
	rule.UsesAI = detectUsesAI(rule.LuaCode)

	// Validate Lua syntax
	if err := h.Engine.ValidateLua(rule.LuaCode); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid Lua code",
			"details": err.Error(),
		})
	}

	if err := h.DB.UpdateRule(c.Context(), &rule); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(rule)
}

// DeleteRule deletes a rule.
func (h *RuleHandlers) DeleteRule(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid rule ID"})
	}

	if err := h.DB.DeleteRule(c.Context(), h.tenantID(c), id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// TestRule evaluates a rule against a test email context.
func (h *RuleHandlers) TestRule(c *fiber.Ctx) error {
	var req struct {
		LuaCode string              `json:"lua_code"`
		Email   models.EmailContext  `json:"email"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	rule := &models.Rule{
		Name:    "test",
		LuaCode: req.LuaCode,
	}

	result, err := h.Engine.TestRule(rule, &req.Email)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "rule evaluation failed",
			"details": err.Error(),
		})
	}

	return c.JSON(result)
}

// ValidateRule checks Lua syntax without executing.
func (h *RuleHandlers) ValidateRule(c *fiber.Ctx) error {
	var req struct {
		LuaCode string `json:"lua_code"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if err := h.Engine.ValidateLua(req.LuaCode); err != nil {
		return c.JSON(fiber.Map{
			"valid":   false,
			"error":   err.Error(),
		})
	}

	return c.JSON(fiber.Map{"valid": true})
}

// ListSuggestedRules returns unapproved auto-learned rules.
func (h *RuleHandlers) ListSuggestedRules(c *fiber.Ctx) error {
	rules, err := h.DB.ListSuggestedRules(c.Context(), h.tenantID(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if rules == nil {
		rules = []models.Rule{}
	}
	return c.JSON(rules)
}

// ApproveRule marks a suggested rule as approved.
func (h *RuleHandlers) ApproveRule(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid rule ID"})
	}

	if err := h.DB.ApproveRule(c.Context(), h.tenantID(c), id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "rule approved"})
}

// ListExecutionLogs returns recent rule execution logs.
func (h *RuleHandlers) ListExecutionLogs(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 50)
	if limit > 500 {
		limit = 500
	}

	logs, err := h.DB.ListExecutionLogs(c.Context(), h.tenantID(c), limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if logs == nil {
		logs = []models.RuleExecutionLog{}
	}
	return c.JSON(logs)
}

// DryRunRule evaluates a rule against all INBOX emails without executing actions.
func (h *RuleHandlers) DryRunRule(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid rule ID"})
	}

	rule, err := h.DB.GetRule(c.Context(), h.tenantID(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "rule not found"})
	}

	// If lua_code or limit is provided in the request body, use them.
	var body struct {
		LuaCode string `json:"lua_code"`
		Limit   int    `json:"limit"`
	}
	_ = c.BodyParser(&body)
	if body.LuaCode != "" {
		rule.LuaCode = body.LuaCode
	}

	// Create a cancellable context and register it for external cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opKey := fmt.Sprintf("tenant:%d:rule:%d", h.tenantID(c), id)
	h.activeOps.Store(opKey, cancel)
	defer h.activeOps.Delete(opKey)

	// Fetch all accounts for this tenant
	accounts, err := h.DB.ListAccounts(ctx, h.tenantID(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to list accounts"})
	}

	if len(accounts) == 0 {
		return c.JSON(models.DryRunResult{
			TotalScanned: 0,
			TotalMatched: 0,
			Matches:      []models.DryRunMatch{},
		})
	}

	// Get full account details (with credentials) for IMAP connection
	var result models.DryRunResult
	result.Matches = []models.DryRunMatch{}

	for _, acc := range accounts {
		if ctx.Err() != nil {
			break
		}
		if !acc.Active {
			continue
		}

		fullAcc, err := h.DB.GetAccount(ctx, h.tenantID(c), acc.ID)
		if err != nil {
			log.Warn().Err(err).Int64("account_id", acc.ID).Msg("failed to get account for dry-run")
			continue
		}

		emails, err := fetchINBOXEmails(fullAcc, body.Limit)
		if err != nil {
			log.Warn().Err(err).Int64("account_id", acc.ID).Msg("failed to fetch INBOX emails for dry-run")
			continue
		}

		for _, email := range emails {
			if ctx.Err() != nil {
				break
			}
			result.TotalScanned++
			ruleResult, err := h.Engine.Evaluate(rule, &email)
			if err != nil {
				continue
			}
			if ruleResult.Action != "skip" {
				result.TotalMatched++
				result.Matches = append(result.Matches, models.DryRunMatch{
					MessageID:     email.MessageID,
					Subject:       email.Subject,
					SenderAddress: email.SenderAddress,
					Action:        ruleResult.Action,
					Target:        ruleResult.Target,
					Reason:        ruleResult.Reason,
				})
			}
		}
	}

	if ctx.Err() != nil {
		result.Cancelled = true
	}

	return c.JSON(result)
}

// CancelOperation cancels an in-progress dry-run or execute for a rule.
func (h *RuleHandlers) CancelOperation(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid rule ID"})
	}

	opKey := fmt.Sprintf("tenant:%d:rule:%d", h.tenantID(c), id)
	if cancelFn, ok := h.activeOps.LoadAndDelete(opKey); ok {
		cancelFn.(context.CancelFunc)()
		log.Info().Int64("rule_id", id).Int64("tenant_id", h.tenantID(c)).Msg("operation cancelled by user")
		return c.JSON(fiber.Map{"message": "operation cancelled"})
	}

	return c.JSON(fiber.Map{"message": "no active operation found"})
}

// ExecuteRule evaluates a rule against all INBOX emails and performs the actions.
func (h *RuleHandlers) ExecuteRule(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid rule ID"})
	}

	rule, err := h.DB.GetRule(c.Context(), h.tenantID(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "rule not found"})
	}

	// If lua_code or limit is provided in the request body, use them.
	var body struct {
		LuaCode string `json:"lua_code"`
		Limit   int    `json:"limit"`
	}
	_ = c.BodyParser(&body)
	if body.LuaCode != "" {
		rule.LuaCode = body.LuaCode
	}

	// Create a cancellable context and register it for external cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opKey := fmt.Sprintf("tenant:%d:rule:%d", h.tenantID(c), id)
	h.activeOps.Store(opKey, cancel)
	defer h.activeOps.Delete(opKey)

	accounts, err := h.DB.ListAccounts(ctx, h.tenantID(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to list accounts"})
	}

	if len(accounts) == 0 {
		return c.JSON(models.ExecuteResult{
			TotalScanned:  0,
			TotalExecuted: 0,
			TotalFailed:   0,
			Results:       []models.DryRunMatch{},
		})
	}

	var result models.ExecuteResult
	result.Results = []models.DryRunMatch{}

	executor := imapactions.New(h.DB, nil)
	defer executor.Close()

	for _, acc := range accounts {
		if ctx.Err() != nil {
			break
		}
		if !acc.Active {
			continue
		}

		fullAcc, err := h.DB.GetAccount(ctx, h.tenantID(c), acc.ID)
		if err != nil {
			log.Warn().Err(err).Int64("account_id", acc.ID).Msg("failed to get account for execute")
			continue
		}

		emails, err := fetchINBOXEmails(fullAcc, body.Limit)
		if err != nil {
			log.Warn().Err(err).Int64("account_id", acc.ID).Msg("failed to fetch INBOX emails for execute")
			result.Errors = append(result.Errors, fmt.Sprintf("account %d: %s", acc.ID, err.Error()))
			continue
		}

		for _, email := range emails {
			if ctx.Err() != nil {
				break
			}
			result.TotalScanned++
			ruleResult, err := h.Engine.Evaluate(rule, &email)
			if err != nil {
				continue
			}
			if ruleResult.Action == "skip" {
				continue
			}

			match := models.DryRunMatch{
				MessageID:     email.MessageID,
				Subject:       email.Subject,
				SenderAddress: email.SenderAddress,
				Action:        ruleResult.Action,
				Target:        ruleResult.Target,
				Reason:        ruleResult.Reason,
			}

			// Execute the action
			var actionErr error
			switch ruleResult.Action {
			case "delete", "move", "archive", "flag":
				actionErr = executor.ExecuteActionByMessageID(ctx, acc.ID, email.MessageID, ruleResult.Action, ruleResult.Target)
			case "defer":
				if ruleResult.Delay > 0 {
					deferred := &models.DeferredAction{
						RuleID:    rule.ID,
						AccountID: acc.ID,
						MessageID: email.MessageID,
						Action:    ruleResult.Target,
						Target:    ruleResult.Target,
						ExecuteAt: time.Now().Add(time.Duration(ruleResult.Delay) * time.Second),
					}
					actionErr = h.DB.CreateDeferredAction(ctx, deferred)
				}
			case "keep", "notify":
				// No IMAP action needed
			}

			if actionErr != nil {
				result.TotalFailed++
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", email.MessageID, actionErr.Error()))
			} else {
				result.TotalExecuted++
			}

			// Log execution
			execLog := &models.RuleExecutionLog{
				RuleID:    rule.ID,
				AccountID: acc.ID,
				MessageID: email.MessageID,
				Subject:   email.Subject,
				Sender:    email.SenderAddress,
				Action:    ruleResult.Action,
				Target:    ruleResult.Target,
				Success:   actionErr == nil,
			}
			if actionErr != nil {
				execLog.Error = actionErr.Error()
			}
			_ = h.DB.LogExecution(ctx, execLog)

			result.Results = append(result.Results, match)
		}
	}

	if ctx.Err() != nil {
		result.Cancelled = true
	}

	return c.JSON(result)
}

// ListDeferredActions returns all pending deferred actions for the tenant.
func (h *RuleHandlers) ListDeferredActions(c *fiber.Ctx) error {
	actions, err := h.DB.ListDeferredActions(c.Context(), h.tenantID(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// Enrich with rule names
	type enrichedAction struct {
		models.DeferredAction
		RuleName string `json:"rule_name"`
		Subject  string `json:"subject,omitempty"`
	}

	// Cache rule names
	ruleNames := make(map[int64]string)
	var enriched []enrichedAction

	for _, a := range actions {
		name, ok := ruleNames[a.RuleID]
		if !ok {
			if r, err := h.DB.GetRule(c.Context(), h.tenantID(c), a.RuleID); err == nil {
				name = r.Name
			} else {
				name = fmt.Sprintf("Rule #%d", a.RuleID)
			}
			ruleNames[a.RuleID] = name
		}
		enriched = append(enriched, enrichedAction{
			DeferredAction: a,
			RuleName:       name,
		})
	}

	if enriched == nil {
		enriched = []enrichedAction{}
	}

	return c.JSON(fiber.Map{"deferred_actions": enriched})
}

// CancelDeferredAction cancels a pending deferred action.
func (h *RuleHandlers) CancelDeferredAction(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid action ID"})
	}

	if err := h.DB.CancelDeferredAction(c.Context(), h.tenantID(c), id); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "deferred action cancelled"})
}

// ReorderRules updates rule priorities based on the provided order.
func (h *RuleHandlers) ReorderRules(c *fiber.Ctx) error {
	var req struct {
		RuleIDs []int64 `json:"rule_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if len(req.RuleIDs) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "rule_ids is required"})
	}

	if err := h.DB.ReorderRules(c.Context(), h.tenantID(c), req.RuleIDs); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "rules reordered"})
}

// fetchINBOXEmails connects to an IMAP account and fetches messages from INBOX.
// If limit > 0, only the most recent `limit` messages are fetched.
func fetchINBOXEmails(account *models.Account, limit int) ([]models.EmailContext, error) {
	host := account.IMAPHost
	port := account.IMAPPort
	if port == 0 {
		port = 993
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	opts := &imapclient.Options{
		TLSConfig: &tls.Config{
			ServerName: host,
		},
	}

	var client *imapclient.Client
	var err error

	if account.IMAPTLS {
		client, err = imapclient.DialTLS(addr, opts)
	} else {
		conn, dialErr := net.DialTimeout("tcp", addr, 30*time.Second)
		if dialErr != nil {
			return nil, fmt.Errorf("dial %s: %w", addr, dialErr)
		}
		client = imapclient.New(conn, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}

	if err := client.WaitGreeting(); err != nil {
		client.Close()
		return nil, fmt.Errorf("greeting from %s: %w", addr, err)
	}

	username := account.Username
	if username == "" {
		username = account.Email
	}

	loginCmd := client.Login(username, account.Password)
	if err := loginCmd.Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("login to %s: %w", addr, err)
	}

	defer func() {
		logoutCmd := client.Logout()
		_ = logoutCmd.Wait()
		client.Close()
	}()

	// Select INBOX
	selectCmd := client.Select("INBOX", nil)
	mbox, err := selectCmd.Wait()
	if err != nil {
		return nil, fmt.Errorf("select INBOX: %w", err)
	}

	if mbox.NumMessages == 0 {
		return nil, nil
	}

	// Determine fetch range based on limit
	start := uint32(1)
	if limit > 0 && uint32(limit) < mbox.NumMessages {
		start = mbox.NumMessages - uint32(limit) + 1
	}

	seqSet := imap.SeqSet{}
	seqSet.AddRange(start, mbox.NumMessages)

	fetchOptions := &imap.FetchOptions{
		Envelope: true,
		Flags:    true,
		UID:      true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierHeader, Peek: true},
			{Specifier: imap.PartSpecifierText, Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: 2048}},
		},
		BodyStructure: &imap.FetchItemBodyStructure{Extended: false},
	}

	fetchCmd := client.Fetch(seqSet, fetchOptions)
	defer fetchCmd.Close()

	var emails []models.EmailContext

	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		buf, err := msg.Collect()
		if err != nil {
			continue
		}

		if buf.Envelope == nil {
			continue
		}

		env := buf.Envelope

		var senderName, senderAddr string
		if len(env.From) > 0 {
			senderName = env.From[0].Name
			senderAddr = env.From[0].Addr()
		}

		var recipients []string
		for _, addr := range env.To {
			recipients = append(recipients, addr.Addr())
		}
		for _, addr := range env.Cc {
			recipients = append(recipients, addr.Addr())
		}

		headers := make(map[string]string)
		var bodyPreview string

		for _, section := range buf.BodySection {
			if section.Section.Specifier == imap.PartSpecifierHeader && len(section.Bytes) > 0 {
				headers = parseHeaders(section.Bytes)
			}
			if section.Section.Specifier == imap.PartSpecifierText && len(section.Bytes) > 0 {
				bodyPreview = string(section.Bytes)
				if len(bodyPreview) > 500 {
					bodyPreview = bodyPreview[:500]
				}
			}
		}

		var attachmentNames, attachmentTypes []string
		hasAttachments := false
		if buf.BodyStructure != nil {
			attachmentNames, attachmentTypes = extractAttachmentInfo(buf.BodyStructure)
			hasAttachments = len(attachmentNames) > 0 || len(attachmentTypes) > 0
		}

		msgDate := env.Date
		if msgDate.IsZero() {
			msgDate = buf.InternalDate
		}

		ageSeconds := time.Since(msgDate).Seconds()
		if ageSeconds < 0 {
			ageSeconds = 0
		}

		emails = append(emails, models.EmailContext{
			MessageID:       env.MessageID,
			Subject:         env.Subject,
			SenderName:      senderName,
			SenderAddress:   senderAddr,
			Recipients:      recipients,
			Date:            msgDate,
			AgeSeconds:      ageSeconds,
			BodyPreview:     bodyPreview,
			HasAttachments:  hasAttachments,
			AttachmentNames: attachmentNames,
			AttachmentTypes: attachmentTypes,
			Headers:         headers,
			Folder:          "INBOX",
			AccountID:       account.ID,
			HasImages:       hasImageAttachments(attachmentTypes),
		})
	}

	return emails, nil
}

// parseHeaders extracts key headers from raw header bytes.
func parseHeaders(raw []byte) map[string]string {
	headers := make(map[string]string)

	entity, err := gomessage.Read(strings.NewReader(string(raw) + "\r\n\r\n"))
	if err != nil {
		return parseHeadersSimple(raw)
	}

	headerFields := entity.Header.Fields()
	for headerFields.Next() {
		key := strings.ToLower(headerFields.Key())
		val := headerFields.Value()
		headers[key] = val
	}

	return headers
}

func parseHeadersSimple(raw []byte) map[string]string {
	headers := make(map[string]string)
	lines := strings.Split(string(raw), "\n")
	var currentKey, currentVal string

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			currentVal += " " + strings.TrimSpace(line)
			continue
		}
		if currentKey != "" {
			headers[strings.ToLower(currentKey)] = currentVal
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			currentKey = strings.TrimSpace(parts[0])
			currentVal = strings.TrimSpace(parts[1])
		}
	}
	if currentKey != "" {
		headers[strings.ToLower(currentKey)] = currentVal
	}
	return headers
}

func extractAttachmentInfo(bs imap.BodyStructure) (names []string, types []string) {
	switch s := bs.(type) {
	case *imap.BodyStructureSinglePart:
		disp := s.Disposition()
		if disp != nil {
			dispVal := strings.ToLower(disp.Value)
			if dispVal == "attachment" || dispVal == "inline" {
				ct := s.MediaType()
				types = append(types, ct)
				filename := s.Filename()
				if filename != "" {
					names = append(names, filename)
				}
			}
		}
	case *imap.BodyStructureMultiPart:
		for _, child := range s.Children {
			cn, ct := extractAttachmentInfo(child)
			names = append(names, cn...)
			types = append(types, ct...)
		}
	}
	return
}

func hasImageAttachments(attachmentTypes []string) bool {
	for _, ct := range attachmentTypes {
		if strings.HasPrefix(strings.ToLower(ct), "image/") {
			return true
		}
	}
	return false
}
