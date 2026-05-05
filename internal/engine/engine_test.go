package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/djpadz/email-automation/internal/models"
)

// sampleEmail returns a basic email context for testing.
func sampleEmail(opts ...func(*models.EmailContext)) *models.EmailContext {
	e := &models.EmailContext{
		MessageID:     "test-001",
		Subject:       "Your order has shipped",
		SenderName:    "Amazon",
		SenderAddress: "ship-confirm@amazon.com",
		Recipients:    []string{"user@example.com"},
		Date:          time.Now().Add(-25 * time.Hour),
		AgeSeconds:    25 * 3600,
		BodyPreview:   "Your package is on its way...",
		HasAttachments: false,
		AttachmentNames: []string{},
		AttachmentTypes: []string{},
		Headers:        map[string]string{},
		Folder:         "INBOX",
		AccountID:      1,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func withSender(name, addr string) func(*models.EmailContext) {
	return func(e *models.EmailContext) {
		e.SenderName = name
		e.SenderAddress = addr
	}
}

func withSubject(s string) func(*models.EmailContext) {
	return func(e *models.EmailContext) {
		e.Subject = s
	}
}

func withAge(seconds float64) func(*models.EmailContext) {
	return func(e *models.EmailContext) {
		e.AgeSeconds = seconds
		e.Date = time.Now().Add(-time.Duration(seconds) * time.Second)
	}
}

func withAttachments(names, types []string) func(*models.EmailContext) {
	return func(e *models.EmailContext) {
		e.AttachmentNames = names
		e.AttachmentTypes = types
		e.HasAttachments = len(names) > 0 || len(types) > 0
	}
}

func withBody(body string) func(*models.EmailContext) {
	return func(e *models.EmailContext) {
		e.BodyPreview = body
	}
}

func withHeaders(h map[string]string) func(*models.EmailContext) {
	return func(e *models.EmailContext) {
		e.Headers = h
	}
}

func TestEngineEvaluate_Skip(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name:    "test-skip",
		LuaCode: `return skip()`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "skip" {
		t.Errorf("expected skip, got %s", result.Action)
	}
}

func TestEngineEvaluate_Delete(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name:    "test-delete",
		LuaCode: `return delete("test reason")`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "delete" {
		t.Errorf("expected delete, got %s", result.Action)
	}
	if result.Reason != "test reason" {
		t.Errorf("expected reason 'test reason', got '%s'", result.Reason)
	}
}

func TestEngineEvaluate_Move(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name:    "test-move",
		LuaCode: `return move("@TestFolder", "moved it")`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "move" {
		t.Errorf("expected move, got %s", result.Action)
	}
	if result.Target != "@TestFolder" {
		t.Errorf("expected target '@TestFolder', got '%s'", result.Target)
	}
}

func TestEngineEvaluate_Archive(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name:    "test-archive",
		LuaCode: `return archive("archived")`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "archive" {
		t.Errorf("expected archive, got %s", result.Action)
	}
}

func TestEngineEvaluate_Keep(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name:    "test-keep",
		LuaCode: `return keep("keeping it")`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "keep" {
		t.Errorf("expected keep, got %s", result.Action)
	}
}

func TestEngineEvaluate_Notify(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name:    "test-notify",
		LuaCode: `return notify("hello world", "notified")`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "notify" {
		t.Errorf("expected notify, got %s", result.Action)
	}
	if result.Target != "hello world" {
		t.Errorf("expected target 'hello world', got '%s'", result.Target)
	}
}

func TestEngineEvaluate_DeferAction(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name:    "test-defer",
		LuaCode: `return move_after("@Later", 3600, "deferred move")`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "defer" {
		t.Errorf("expected defer, got %s", result.Action)
	}
	if result.Delay != 3600 {
		t.Errorf("expected delay 3600, got %d", result.Delay)
	}
}

func TestEngineEvaluate_DeleteAfter(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name:    "test-delete-after",
		LuaCode: `return delete_after(7200, "delete later")`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "defer" {
		t.Errorf("expected defer, got %s", result.Action)
	}
	if result.Delay != 7200 {
		t.Errorf("expected delay 7200, got %d", result.Delay)
	}
}

func TestEngineHelpers_Contains(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-contains",
		LuaCode: `
if contains(email.subject, "shipped") then
    return move("@Shipped")
end
return skip()`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "move" {
		t.Errorf("expected move, got %s", result.Action)
	}
}

func TestEngineHelpers_ContainsAny(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-contains-any",
		LuaCode: `
if contains_any(email.subject, {"tracking", "shipped", "delivered"}) then
    return move("@Orders")
end
return skip()`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "move" {
		t.Errorf("expected move, got %s", result.Action)
	}
}

func TestEngineHelpers_DomainOf(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-domain-of",
		LuaCode: `
if domain_of(email.sender_address) == "amazon.com" then
    return move("@Amazon")
end
return skip()`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "move" {
		t.Errorf("expected move, got %s", result.Action)
	}
}

func TestEngineHelpers_OlderThanHours(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-older-than",
		LuaCode: `
if older_than_hours(24) then
    return delete("old")
end
return skip()`,
	}

	// 25 hours old — should match
	result, err := eng.Evaluate(rule, sampleEmail(withAge(25*3600)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "delete" {
		t.Errorf("expected delete for 25h old email, got %s", result.Action)
	}

	// 1 hour old — should not match
	result, err = eng.Evaluate(rule, sampleEmail(withAge(3600)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "skip" {
		t.Errorf("expected skip for 1h old email, got %s", result.Action)
	}
}

func TestEngineHelpers_StartsEndsWith(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-starts-ends",
		LuaCode: `
if starts_with(email.sender_address, "ship") and ends_with(email.sender_address, "amazon.com") then
    return move("@Amazon")
end
return skip()`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "move" {
		t.Errorf("expected move, got %s", result.Action)
	}
}

func TestEngineHelpers_IsReply(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-is-reply",
		LuaCode: `
if is_reply() then
    return archive("reply")
end
return skip()`,
	}

	// Not a reply
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "skip" {
		t.Errorf("expected skip for non-reply, got %s", result.Action)
	}

	// Is a reply
	result, err = eng.Evaluate(rule, sampleEmail(withSubject("Re: Your order")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "archive" {
		t.Errorf("expected archive for reply, got %s", result.Action)
	}
}

func TestEngineHelpers_HasIcs(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-has-ics",
		LuaCode: `
if has_ics() then
    return archive("calendar")
end
return skip()`,
	}

	// No attachments
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "skip" {
		t.Errorf("expected skip without ics, got %s", result.Action)
	}

	// With .ics attachment
	result, err = eng.Evaluate(rule, sampleEmail(withAttachments(
		[]string{"invite.ics"},
		[]string{"text/calendar"},
	)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "archive" {
		t.Errorf("expected archive with ics, got %s", result.Action)
	}
}

func TestEngineValidateLua_Valid(t *testing.T) {
	eng := New()
	err := eng.ValidateLua(`return skip()`)
	if err != nil {
		t.Errorf("expected valid lua, got error: %v", err)
	}
}

func TestEngineValidateLua_Invalid(t *testing.T) {
	eng := New()
	err := eng.ValidateLua(`this is not valid lua !!!`)
	if err == nil {
		t.Error("expected error for invalid lua, got nil")
	}
}

func TestEngineEvaluateAll_Priority(t *testing.T) {
	eng := New()
	rules := []models.Rule{
		{
			ID:       1,
			Name:     "first-skip",
			LuaCode:  `return skip()`,
			Priority: 1,
		},
		{
			ID:       2,
			Name:     "second-match",
			LuaCode:  `return move("@Test", "matched")`,
			Priority: 2,
		},
		{
			ID:       3,
			Name:     "third-never",
			LuaCode:  `return delete("should not reach")`,
			Priority: 3,
		},
	}

	result, matchedRule, err := eng.EvaluateAll(rules, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "move" {
		t.Errorf("expected move, got %s", result.Action)
	}
	if matchedRule.ID != 2 {
		t.Errorf("expected rule ID 2, got %d", matchedRule.ID)
	}
}

func TestEngineEvaluateAll_NoMatch(t *testing.T) {
	eng := New()
	rules := []models.Rule{
		{ID: 1, Name: "skip1", LuaCode: `return skip()`},
		{ID: 2, Name: "skip2", LuaCode: `return skip()`},
	}

	result, _, err := eng.EvaluateAll(rules, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "skip" {
		t.Errorf("expected skip, got %s", result.Action)
	}
}

func TestEngineSandbox_NoDangerousFunctions(t *testing.T) {
	eng := New()

	dangerous := []struct {
		name string
		code string
	}{
		{"dofile", `dofile("/etc/passwd")`},
		{"loadfile", `loadfile("/etc/passwd")`},
		{"os.execute", `os.execute("ls")`},
		{"io.open", `io.open("/etc/passwd")`},
	}

	for _, tc := range dangerous {
		t.Run(tc.name, func(t *testing.T) {
			rule := &models.Rule{Name: "dangerous", LuaCode: tc.code}
			_, err := eng.Evaluate(rule, sampleEmail())
			if err == nil {
				t.Errorf("expected error for dangerous function %s, got nil", tc.name)
			}
		})
	}
}

// TestMigratedRules tests all 18 migrated Lua rules against sample emails.
func TestMigratedRules(t *testing.T) {
	eng := New()
	rulesDir := "../../rules"

	tests := []struct {
		ruleFile string
		email    *models.EmailContext
		expected string
		target   string
	}{
		// Amazon — old enough to move
		{
			"amazon.lua",
			sampleEmail(withSender("Amazon", "ship-confirm@amazon.com"), withSubject("Your order has shipped"), withAge(25*3600)),
			"move", "@Amazon",
		},
		// Amazon — too young, keep
		{
			"amazon.lua",
			sampleEmail(withSender("Amazon", "ship-confirm@amazon.com"), withSubject("Your order has shipped"), withAge(3600)),
			"keep", "",
		},
		// Amazon — wrong sender
		{
			"amazon.lua",
			sampleEmail(withSender("Bob", "bob@gmail.com"), withSubject("Your order has shipped")),
			"skip", "",
		},
		// Atlassian
		{
			"atlassian.lua",
			sampleEmail(withSender("Atlassian", "noreply@atlassian.com"), withSubject("Your payment has been processed")),
			"move", "@Receipts & Invoices",
		},
		// Calendar responses — old with ics
		{
			"cal_responses.lua",
			sampleEmail(withSubject("Accepted: Team Meeting"), withAge(25*3600), withAttachments([]string{"invite.ics"}, []string{"text/calendar"})),
			"archive", "",
		},
		// Calendar responses — too young
		{
			"cal_responses.lua",
			sampleEmail(withSubject("Accepted: Team Meeting"), withAge(3600), withAttachments([]string{"invite.ics"}, []string{"text/calendar"})),
			"skip", "",
		},
		// Contabo
		{
			"contabo.lua",
			sampleEmail(withSender("Contabo", "billing@contabo.com"), withSubject("Credit card payment confirmation")),
			"move", "@Receipts",
		},
		// CrowdStrike
		{
			"crowdstrike.lua",
			sampleEmail(withSender("CrowdStrike", "intel@crowdstrike.com"), withSubject("CSWR-2025-001 Weekly Report")),
			"move", "@SaneNews",
		},
		// DMARC reports
		{
			"dmarc_reports.lua",
			sampleEmail(withSubject("Report Domain: example.com")),
			"move", "@30DayTrash",
		},
		// Headway — old enough
		{
			"headway.lua",
			sampleEmail(withSender("Headway", "noreply@e.headway.co"), withSubject("Reminder: Appointment on 3/24"), withAge(3*86400)),
			"delete", "",
		},
		// Insight marketing
		{
			"insight_marketing.lua",
			sampleEmail(withSender("Insight", "promo@mktg.insight.com")),
			"move", "@SaneNews",
		},
		// IronPort alerts
		{
			"ironport.lua",
			sampleEmail(withSender("IronPort", "alert@mx1.ctb.padz.net"), withSubject("IronPort Alert")),
			"move", "@Ironport",
		},
		// Login codes — old enough
		{
			"login_codes.lua",
			sampleEmail(withSubject("Your verification code is 123456"), withAge(2*3600)),
			"delete", "",
		},
		// Login codes — too young
		{
			"login_codes.lua",
			sampleEmail(withSubject("Your verification code is 123456"), withAge(1800)),
			"skip", "",
		},
		// Morgan Stanley — old enough
		{
			"morgan_stanley.lua",
			sampleEmail(withSender("MS", "alerts@morganstanley.com"), withSubject("Mobile check deposit confirmation"), withAge(25*3600)),
			"move", "@MorganStanley",
		},
		// rblmon — all clear
		{
			"rblmon.lua",
			sampleEmail(withSender("rblmon", "alert@rblmon.com"), withSubject("No blocks found for your IP")),
			"delete", "",
		},
		// Receipts
		{
			"receipts.lua",
			sampleEmail(withSubject("Order confirmation #12345")),
			"move", "@Receipts",
		},
		// SaneBox — old enough
		{
			"sanebox.lua",
			sampleEmail(withSender("SaneBox", "digest@sanebox.com"), withSubject("Your SaneBox digest"), withAge(25*3600)),
			"delete", "",
		},
		// Scripps video visit — old enough
		{
			"scripps_video_visit.lua",
			sampleEmail(withSender("Scripps", "myscrippsdonotreply@myscripps.org"), withSubject("Video Visit Direct Join Link"), withAge(25*3600)),
			"delete", "",
		},
		// Teams notifications — old enough
		{
			"teams_notifications.lua",
			sampleEmail(withSender("Teams", "no-reply@teams.microsoft.com"), withAge(7*3600)),
			"delete", "",
		},
		// USPS — old enough
		{
			"usps_informed_delivery.lua",
			sampleEmail(withSender("USPS", "informeddelivery@usps.gov"), withAge(2*86400)),
			"move", "@30DayTrash",
		},
		// Venmo — old enough
		{
			"venmo.lua",
			sampleEmail(withSender("Venmo", "venmo@venmo.com"), withSubject("You paid John $25.00"), withAge(3*86400)),
			"move", "@Receipts",
		},
	}

	for _, tc := range tests {
		t.Run(tc.ruleFile+"/"+tc.expected, func(t *testing.T) {
			code, err := os.ReadFile(filepath.Join(rulesDir, tc.ruleFile))
			if err != nil {
				t.Fatalf("failed to read rule file %s: %v", tc.ruleFile, err)
			}

			rule := &models.Rule{
				Name:    tc.ruleFile,
				LuaCode: string(code),
			}

			result, err := eng.Evaluate(rule, tc.email)
			if err != nil {
				t.Fatalf("evaluation error: %v", err)
			}

			if result.Action != tc.expected {
				t.Errorf("expected action %q, got %q (reason: %s)", tc.expected, result.Action, result.Reason)
			}

			if tc.target != "" && result.Target != tc.target {
				t.Errorf("expected target %q, got %q", tc.target, result.Target)
			}
		})
	}
}

func TestEngineEmailContext_AllFields(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-all-fields",
		LuaCode: `
-- Verify all email fields are accessible
assert(email.message_id == "test-001")
assert(email.subject ~= "")
assert(email.sender_name ~= "")
assert(email.sender_address ~= "")
assert(email.sender ~= "")
assert(type(email.recipients) == "table")
assert(type(email.age_seconds) == "number")
assert(email.body_preview ~= "")
assert(type(email.has_attachments) == "boolean")
assert(type(email.attachment_names) == "table")
assert(type(email.attachment_types) == "table")
assert(type(email.headers) == "table")
assert(email.folder == "INBOX")
assert(email.date ~= "")
return keep("all fields verified")`,
	}

	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "keep" {
		t.Errorf("expected keep, got %s (reason: %s)", result.Action, result.Reason)
	}
}

func TestEngineMalformedRule(t *testing.T) {
	eng := New()

	tests := []struct {
		name string
		code string
	}{
		{"syntax error", "if then end end"},
		{"runtime error", "error('boom')"},
		{"nil index", "local x = nil; return x.foo"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rule := &models.Rule{Name: tc.name, LuaCode: tc.code}
			_, err := eng.Evaluate(rule, sampleEmail())
			if err == nil {
				t.Error("expected error for malformed rule, got nil")
			}
		})
	}
}

func TestEngineKiroNamespace_Available(t *testing.T) {
	// Without a kiro client, kiro functions should still be available but return false
	eng := New()
	rule := &models.Rule{
		Name: "test-kiro-no-client",
		LuaCode: `
-- kiro table should exist even without API key
assert(type(kiro) == "table")
assert(type(kiro.classify) == "function")
assert(type(kiro.is_actionable) == "function")

-- Without API key, kiro.classify should return false gracefully
if kiro.classify(email, "Is this important?") then
    return keep("important")
end
return skip()
`,
	}

	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "skip" {
		t.Errorf("expected skip (kiro without API key returns false), got %s", result.Action)
	}
}

func TestEngineKiroNamespace_WithClient(t *testing.T) {
	// With a kiro client (but no real API key), functions should still not panic
	kiroClient := NewKiroClient("", "", "", "")
	eng := NewWithKiro(kiroClient)
	rule := &models.Rule{
		Name: "test-kiro-with-client",
		LuaCode: `
assert(type(kiro) == "table")
-- is_actionable should return false when API key is empty
if kiro.is_actionable(email) then
    return keep("actionable")
end
return archive("not actionable")
`,
	}

	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "archive" {
		t.Errorf("expected archive (kiro without API key returns false), got %s", result.Action)
	}
}

// --- schedule() tests ---

func TestEngineEvaluate_ScheduleMove(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-schedule-move",
		LuaCode: `
schedule(3 * 60 * 60, function()
    move("@Archive")
end)
`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "defer" {
		t.Errorf("expected defer, got %s", result.Action)
	}
	if result.Target != "@Archive" {
		t.Errorf("expected target '@Archive', got '%s'", result.Target)
	}
	if result.Delay != 10800 {
		t.Errorf("expected delay 10800, got %d", result.Delay)
	}
	if result.Reason != "scheduled move" {
		t.Errorf("expected reason 'scheduled move', got '%s'", result.Reason)
	}
}

func TestEngineEvaluate_ScheduleDelete(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-schedule-delete",
		LuaCode: `
schedule(24 * 60 * 60, function()
    delete()
end)
`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "defer" {
		t.Errorf("expected defer, got %s", result.Action)
	}
	if result.Delay != 86400 {
		t.Errorf("expected delay 86400, got %d", result.Delay)
	}
	if result.Reason != "scheduled delete" {
		t.Errorf("expected reason 'scheduled delete', got '%s'", result.Reason)
	}
}

func TestEngineEvaluate_ScheduleArchive(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-schedule-archive",
		LuaCode: `
schedule(30 * 60, function()
    archive()
end)
`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "defer" {
		t.Errorf("expected defer, got %s", result.Action)
	}
	if result.Delay != 1800 {
		t.Errorf("expected delay 1800, got %d", result.Delay)
	}
	if result.Reason != "scheduled archive" {
		t.Errorf("expected reason 'scheduled archive', got '%s'", result.Reason)
	}
}

func TestEngineEvaluate_ScheduleFlag(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-schedule-flag",
		LuaCode: `
schedule(2 * 60 * 60, function()
    flag("important")
end)
`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "defer" {
		t.Errorf("expected defer, got %s", result.Action)
	}
	if result.Target != "important" {
		t.Errorf("expected target 'important', got '%s'", result.Target)
	}
	if result.Delay != 7200 {
		t.Errorf("expected delay 7200, got %d", result.Delay)
	}
}

func TestEngineEvaluate_ScheduleNotify(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-schedule-notify",
		LuaCode: `
schedule(60 * 60, function()
    notify("Reminder: check this email")
end)
`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "defer" {
		t.Errorf("expected defer, got %s", result.Action)
	}
	if result.Target != "Reminder: check this email" {
		t.Errorf("expected target 'Reminder: check this email', got '%s'", result.Target)
	}
	if result.Delay != 3600 {
		t.Errorf("expected delay 3600, got %d", result.Delay)
	}
}

func TestEngineEvaluate_ScheduleNoAction(t *testing.T) {
	eng := New()
	rule := &models.Rule{
		Name: "test-schedule-no-action",
		LuaCode: `
schedule(3600, function()
    -- no action called inside
end)
`,
	}
	_, err := eng.Evaluate(rule, sampleEmail())
	if err == nil {
		t.Fatal("expected error for schedule with no action, got nil")
	}
}

func TestEngineEvaluate_ScheduleConditional(t *testing.T) {
	// Test that schedule works with conditions before it
	eng := New()
	rule := &models.Rule{
		Name: "test-schedule-conditional",
		LuaCode: `
local sender = email.sender_address:lower()
if not ends_with(sender, "amazon.com") then return skip() end

schedule(3 * 60 * 60, function()
    move("@Amazon")
end)
`,
	}

	// Should match amazon sender
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "defer" {
		t.Errorf("expected defer, got %s", result.Action)
	}
	if result.Target != "@Amazon" {
		t.Errorf("expected target '@Amazon', got '%s'", result.Target)
	}

	// Should skip non-amazon sender
	result2, err := eng.Evaluate(rule, sampleEmail(withSender("Other", "test@other.com")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.Action != "skip" {
		t.Errorf("expected skip for non-amazon, got %s", result2.Action)
	}
}

func TestEngineEvaluate_ScheduleDoesNotAffectNormalActions(t *testing.T) {
	// Verify that normal action calls still work after schedule is registered
	eng := New()
	rule := &models.Rule{
		Name: "test-normal-after-schedule",
		LuaCode: `return move("@Test", "normal move")`,
	}
	result, err := eng.Evaluate(rule, sampleEmail())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "move" {
		t.Errorf("expected move, got %s", result.Action)
	}
	if result.Target != "@Test" {
		t.Errorf("expected target '@Test', got '%s'", result.Target)
	}
}
