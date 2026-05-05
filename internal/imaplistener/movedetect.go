package imaplistener

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/models"
	"github.com/djpadz/email-automation/internal/movedetect"
)

// buildUIDMap fetches all UIDs currently in INBOX and stores them in order
// for sequence-number-to-UID mapping during EXPUNGE notifications.
// It also records message locations for move detection lookups.
func (w *worker) buildUIDMap(ctx context.Context, client *imapclient.Client) error {
	criteria := &imap.SearchCriteria{}
	searchCmd := client.UIDSearch(criteria, nil)
	searchData, err := searchCmd.Wait()
	if err != nil {
		return fmt.Errorf("search all UIDs: %w", err)
	}

	allUIDs := searchData.AllUIDs()

	w.uidsMu.Lock()
	w.inboxUIDs = make([]imap.UID, len(allUIDs))
	copy(w.inboxUIDs, allUIDs)
	w.uidsMu.Unlock()

	log.Debug().
		Int64("account_id", w.account.ID).
		Int("uid_count", len(allUIDs)).
		Msg("built INBOX UID map for EXPUNGE tracking")

	// Record message locations for all INBOX messages (needed for move detection)
	w.recordInboxMessageLocations(ctx, client, allUIDs)

	return nil
}

// drainExpungeChannel drains any stale EXPUNGE notifications from the channel.
func (w *worker) drainExpungeChannel() {
	for {
		select {
		case <-w.expungeCh:
		default:
			return
		}
	}
}

// mapSeqNumsToUIDs converts a series of EXPUNGE sequence numbers to UIDs.
// IMAP EXPUNGE notifications are sequential: after each expunge, remaining
// sequence numbers shift down. We process them in order against our local copy.
func (w *worker) mapSeqNumsToUIDs(seqNums []uint32) []imap.UID {
	w.uidsMu.Lock()
	defer w.uidsMu.Unlock()

	var result []imap.UID

	// Work on a copy so we can mutate it
	uidsCopy := make([]imap.UID, len(w.inboxUIDs))
	copy(uidsCopy, w.inboxUIDs)

	for _, seqNum := range seqNums {
		idx := int(seqNum) - 1 // sequence numbers are 1-based
		if idx < 0 || idx >= len(uidsCopy) {
			log.Warn().
				Int64("account_id", w.account.ID).
				Uint32("seq_num", seqNum).
				Int("uid_count", len(uidsCopy)).
				Msg("EXPUNGE sequence number out of range")
			continue
		}

		result = append(result, uidsCopy[idx])
		// Remove the expunged entry - subsequent seqNums reference the shifted list
		uidsCopy = append(uidsCopy[:idx], uidsCopy[idx+1:]...)
	}

	// Update the worker's UID list to reflect the expunges
	w.inboxUIDs = uidsCopy

	return result
}

// handleExpungedMessages processes UIDs that were expunged from INBOX during IDLE.
// For each expunged UID, it looks up the message details from message_locations
// and searches other folders to detect where the message was moved.
func (w *worker) handleExpungedMessages(ctx context.Context, client *imapclient.Client, expungedUIDs []imap.UID) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Logger()

	logger.Info().Int("count", len(expungedUIDs)).Msg("processing expunged messages for move detection")

	for _, uid := range expungedUIDs {
		uidStr := fmt.Sprintf("%d", uid)

		// Look up the message details from our recorded locations
		locs, err := w.db.GetMessageLocations(ctx, w.account.ID, uidStr)
		if err != nil {
			logger.Warn().Err(err).Str("uid", uidStr).Msg("failed to get message location")
			continue
		}

		if len(locs) == 0 {
			logger.Debug().Str("uid", uidStr).Msg("no recorded location for expunged UID, skipping")
			continue
		}

		// Use the first (and typically only) location record
		loc := locs[0]
		if loc.MessageID == "" {
			logger.Debug().Str("uid", uidStr).Msg("no Message-ID for expunged message, skipping")
			continue
		}

		logger.Info().
			Str("uid", uidStr).
			Str("message_id", loc.MessageID).
			Str("sender", loc.Sender).
			Str("subject", loc.Subject).
			Msg("looking for moved message in other folders")

		// Search other folders for this message by Message-ID
		destFolder := w.findMessageInFolders(client, loc.MessageID)
		if destFolder == "" {
			logger.Debug().
				Str("message_id", loc.MessageID).
				Msg("expunged message not found in other folders (likely deleted)")
			continue
		}

		// Part 3: Check if destination is a trash/junk folder - skip rule creation
		if isTrashFolder(destFolder) {
			logger.Info().
				Str("message_id", loc.MessageID).
				Str("destination", destFolder).
				Msg("message moved to trash/junk folder, skipping rule creation")
			continue
		}

		logger.Info().
			Str("message_id", loc.MessageID).
			Str("sender", loc.Sender).
			Str("subject", loc.Subject).
			Str("destination", destFolder).
			Msg("detected move via IDLE EXPUNGE: INBOX → " + destFolder)

		// Record the move
		move := &models.DetectedMove{
			AccountID:  w.account.ID,
			MessageUID: uidStr,
			MessageID:  loc.MessageID,
			Sender:     loc.Sender,
			Subject:    loc.Subject,
			FromFolder: "INBOX",
			ToFolder:   destFolder,
		}

		if err := w.db.RecordDetectedMove(ctx, move); err != nil {
			logger.Error().Err(err).Msg("failed to record detected move")
			continue
		}

		// Check if this move was performed by the rules engine (avoid self-detection)
		processed, err := w.db.IsMessageProcessed(ctx, w.account.ID, uidStr)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to check if message was processed by rules engine")
		}
		if processed {
			logger.Debug().
				Str("message_id", loc.MessageID).
				Msg("skipping rule creation: message was moved by rules engine")
			continue
		}

		// Generate and create a rule based on heuristics
		w.generateAndCreateRuleFromMove(ctx, loc, destFolder)
	}

	return nil
}

// findMessageInFolders searches other folders for a message by Message-ID.
// Returns the folder name where found, or empty string if not found.
func (w *worker) findMessageInFolders(client *imapclient.Client, messageID string) string {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("message_id", messageID).
		Logger()

	// List all folders
	listCmd := client.List("", "*", nil)
	var folders []string
	for {
		data := listCmd.Next()
		if data == nil {
			break
		}
		name := data.Mailbox
		if name == "INBOX" {
			continue
		}
		// Skip \Noselect mailboxes
		noSelect := false
		for _, attr := range data.Attrs {
			if attr == imap.MailboxAttrNoSelect {
				noSelect = true
				break
			}
		}
		if noSelect {
			continue
		}
		// Skip Sent/Drafts folders
		lower := strings.ToLower(name)
		if lower == "drafts" || lower == "sent" || lower == "sent messages" || lower == "sent items" ||
			lower == "[gmail]/sent mail" || lower == "[gmail]/drafts" {
			continue
		}
		folders = append(folders, name)
	}
	if err := listCmd.Close(); err != nil {
		logger.Warn().Err(err).Msg("failed to list folders")
		return ""
	}

	// Search for the message in each folder
	for _, folder := range folders {
		selectCmd := client.Select(folder, nil)
		mbox, err := selectCmd.Wait()
		if err != nil {
			continue
		}
		if mbox.NumMessages == 0 {
			continue
		}

		criteria := &imap.SearchCriteria{
			Header: []imap.SearchCriteriaHeaderField{
				{Key: "Message-ID", Value: messageID},
			},
		}
		searchCmd := client.UIDSearch(criteria, nil)
		searchData, err := searchCmd.Wait()
		if err != nil {
			continue
		}

		uids := searchData.AllUIDs()
		if len(uids) > 0 {
			return folder
		}
	}

	return ""
}

// generateAndCreateRuleFromMove creates an auto-learned rule based on the move.
func (w *worker) generateAndCreateRuleFromMove(ctx context.Context, loc models.MessageLocation, destFolder string) {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("sender", loc.Sender).
		Str("target_folder", destFolder).
		Logger()

	// Check if a similar rule already exists
	rules, err := w.db.ListRules(ctx, w.account.TenantID)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to list rules for duplicate check")
	} else {
		for _, rule := range rules {
			if strings.Contains(rule.LuaCode, loc.Sender) && strings.Contains(rule.LuaCode, destFolder) {
				logger.Debug().
					Str("sender", loc.Sender).
					Str("folder", destFolder).
					Msg("similar rule already exists, skipping")
				return
			}
		}
	}

	// Use the movedetect package's InferRule to generate Lua code
	luaCode := movedetect.InferRule(loc.Sender, loc.Subject, destFolder)

	ruleName := fmt.Sprintf("Auto: %s → %s", truncate(loc.Sender, 30), destFolder)

	rule := &models.Rule{
		TenantID:    w.account.TenantID,
		Name:        ruleName,
		Description: fmt.Sprintf("Auto-detected via IDLE move: INBOX → %s (sender: %s, subject: %s)", destFolder, loc.Sender, truncate(loc.Subject, 50)),
		LuaCode:     luaCode,
		Priority:    1000, // Low priority so manual rules take precedence
		Active:      true,
		Source:      "auto-learned",
		Approved:    true, // Auto-approve since user explicitly moved the message
	}

	if err := w.db.CreateRule(ctx, rule); err != nil {
		logger.Error().Err(err).Msg("failed to create auto-learned rule from IDLE move")
		return
	}

	logger.Info().
		Int64("rule_id", rule.ID).
		Str("rule_name", ruleName).
		Str("lua_code", luaCode).
		Msg("created auto-learned rule from IDLE move detection")
}

// recordInboxMessageLocations fetches envelope data for INBOX messages and records
// their locations in the database for later move detection lookups.
func (w *worker) recordInboxMessageLocations(ctx context.Context, client *imapclient.Client, uids []imap.UID) {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Logger()

	if len(uids) == 0 {
		return
	}

	// Fetch in batches
	batchSize := 100
	for i := 0; i < len(uids); i += batchSize {
		end := i + batchSize
		if end > len(uids) {
			end = len(uids)
		}
		batch := uids[i:end]

		uidSet := imap.UIDSet{}
		for _, uid := range batch {
			uidSet.AddNum(uid)
		}

		fetchOptions := &imap.FetchOptions{
			Envelope: true,
			UID:      true,
		}

		fetchCmd := client.Fetch(uidSet, fetchOptions)
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

			var senderAddr string
			if len(buf.Envelope.From) > 0 {
				senderAddr = buf.Envelope.From[0].Addr()
			}

			loc := &models.MessageLocation{
				AccountID:  w.account.ID,
				MessageUID: fmt.Sprintf("%d", buf.UID),
				Folder:     "INBOX",
				MessageID:  buf.Envelope.MessageID,
				Sender:     sanitizeUTF8(senderAddr),
				Subject:    sanitizeUTF8(buf.Envelope.Subject),
			}
			if err := w.db.UpsertMessageLocation(ctx, loc); err != nil {
				logger.Warn().Err(err).Str("message_id", buf.Envelope.MessageID).Msg("failed to record message location")
			}
		}
		fetchCmd.Close()
	}
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// sanitizeUTF8 removes invalid UTF-8 sequences from a string
// to prevent PostgreSQL encoding errors.
func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	// Replace invalid bytes with the Unicode replacement character
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune('\uFFFD')
			i++
		} else {
			b.WriteRune(r)
			i += size
		}
	}
	return b.String()
}
