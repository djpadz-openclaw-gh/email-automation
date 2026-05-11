package movedetect

import (
	"fmt"
	"regexp"
	"strings"
)

// InferRule generates Lua code for a rule based on the sender and subject of a moved message.
// It handles complex senders (bounces, hex-heavy addresses) by falling back to subject keywords.
func InferRule(sender, subject, destFolder string) string {
	if isComplexSender(sender) {
		return inferFromSubject(subject, destFolder)
	}
	return inferFromSender(sender, destFolder)
}

// inferFromSender creates a simple sender-match rule.
func inferFromSender(sender, destFolder string) string {
	// Escape any quotes in the sender address
	escapedSender := strings.ReplaceAll(sender, `"`, `\"`)
	return fmt.Sprintf(`-- Auto-learned: move emails from %s to %s
if email.sender == "%s" then
  move("%s")
end`, sender, destFolder, escapedSender, destFolder)
}

// inferFromSubject creates a rule based on subject keywords.
func inferFromSubject(subject, destFolder string) string {
	keywords := extractKeywords(subject)

	if len(keywords) == 0 {
		// Fallback: use the sender domain if we can extract one
		return fmt.Sprintf(`-- Auto-learned: move to %s (no keywords extracted)
-- Subject was: %s
-- TODO: refine this rule manually
if false then
  move("%s")
end`, destFolder, truncate(subject, 80), destFolder)
	}

	// Build Lua conditions
	var conditions []string
	for _, kw := range keywords {
		escaped := strings.ReplaceAll(kw, `"`, `\"`)
		conditions = append(conditions, fmt.Sprintf(`contains(email.subject, "%s")`, escaped))
	}

	condStr := strings.Join(conditions, " or ")

	return fmt.Sprintf(`-- Auto-learned: move to %s based on subject keywords
if %s then
  move("%s")
end`, destFolder, condStr, destFolder)
}

// isComplexSender determines if a sender address is "complex" (bounces, automated, hex-heavy).
func isComplexSender(sender string) bool {
	if sender == "" {
		return true
	}

	lower := strings.ToLower(sender)

	// Check for embedded email patterns (e.g., bounce-user=domain.com@sender.com)
	if strings.Contains(lower, "=") && strings.Count(lower, "@") >= 1 {
		return true
	}

	// Check for bounce- prefix
	if strings.HasPrefix(lower, "bounce-") || strings.HasPrefix(lower, "bounces-") {
		return true
	}

	// Check for VERP-style addresses (Variable Envelope Return Path)
	if strings.Contains(lower, "+") && (strings.Contains(lower, "bounce") || strings.Contains(lower, "return")) {
		return true
	}

	// Extract local part (before @)
	localPart := lower
	if idx := strings.Index(lower, "@"); idx > 0 {
		localPart = lower[:idx]
	}

	// Check for lots of hyphens (more than 3)
	if strings.Count(localPart, "-") > 3 {
		return true
	}

	// Check for hex-heavy content (more than 8 hex chars in a row)
	hexPattern := regexp.MustCompile(`[0-9a-f]{8,}`)
	if hexPattern.MatchString(localPart) {
		return true
	}

	// Check for UUID-like patterns
	uuidPattern := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}`)
	if uuidPattern.MatchString(localPart) {
		return true
	}

	// Check for noreply/no-reply patterns with random suffixes
	if (strings.Contains(localPart, "noreply") || strings.Contains(localPart, "no-reply")) && len(localPart) > 15 {
		return true
	}

	return false
}

// extractKeywords pulls significant words from a subject line.
func extractKeywords(subject string) []string {
	// Remove common prefixes
	cleaned := subject
	prefixes := []string{"Re:", "RE:", "Fwd:", "FWD:", "Fw:", "FW:", "AW:", "SV:", "TR:"}
	for _, p := range prefixes {
		cleaned = strings.TrimPrefix(cleaned, p)
		cleaned = strings.TrimPrefix(cleaned, strings.ToLower(p))
	}
	cleaned = strings.TrimSpace(cleaned)

	// Remove brackets content like [EXTERNAL], [List-Name], etc.
	bracketPattern := regexp.MustCompile(`\[.*?\]`)
	cleaned = bracketPattern.ReplaceAllString(cleaned, "")
	cleaned = strings.TrimSpace(cleaned)

	// Split into words
	words := strings.Fields(cleaned)

	// Filter out common/stop words and short words
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "shall": true, "can": true,
		"for": true, "and": true, "nor": true, "but": true, "or": true,
		"yet": true, "so": true, "at": true, "by": true, "from": true,
		"in": true, "into": true, "of": true, "on": true, "to": true,
		"with": true, "that": true, "this": true, "these": true, "those": true,
		"it": true, "its": true, "you": true, "your": true, "we": true,
		"our": true, "my": true, "me": true, "i": true, "he": true,
		"she": true, "they": true, "them": true, "their": true,
		"not": true, "no": true, "all": true, "each": true, "every": true,
		"new": true, "just": true, "about": true, "up": true, "out": true,
		"if": true, "then": true, "than": true, "when": true, "what": true,
		"how": true, "who": true, "which": true, "where": true, "why": true,
		"here": true, "there": true, "now": true, "also": true,
		"-": true, "–": true, "—": true, "|": true,
	}

	var significant []string
	for _, word := range words {
		lower := strings.ToLower(word)
		// Remove punctuation from edges
		lower = strings.Trim(lower, ".,!?;:\"'()[]{}#*")

		if len(lower) < 3 {
			continue
		}
		if stopWords[lower] {
			continue
		}
		// Skip pure numbers
		if isNumeric(lower) {
			continue
		}
		significant = append(significant, lower)
	}

	// Return up to 3 keywords
	if len(significant) > 3 {
		significant = significant[:3]
	}

	return significant
}

// isNumeric checks if a string is all digits.
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
