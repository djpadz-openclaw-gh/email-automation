package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/djpadz/email-automation/internal/models"
)

// Engine evaluates Lua rules against email messages in a sandboxed environment.
type Engine struct{}

// New creates a new rule engine.
func New() *Engine {
	return &Engine{}
}

// Evaluate runs a single rule's Lua code against an email context.
// Returns the rule result or an error if execution fails.
func (e *Engine) Evaluate(rule *models.Rule, email *models.EmailContext) (*models.RuleResult, error) {
	L := lua.NewState(lua.Options{
		SkipOpenLibs: true,
	})
	defer L.Close()

	// Open only safe libraries (no os, io, debug)
	openSafeLibs(L)

	// Register helper functions
	registerHelpers(L)

	// Set up the email context as a Lua table
	emailTable := emailToLua(L, email)
	L.SetGlobal("email", emailTable)

	// Set up the result table
	L.DoString(`
		__result = { action = "skip", target = "", delay = 0, reason = "" }
	`)

	// Register action functions
	registerActions(L)

	// Execute the rule
	if err := L.DoString(rule.LuaCode); err != nil {
		return nil, fmt.Errorf("lua execution error: %w", err)
	}

	// Extract result
	result := extractResult(L)
	return result, nil
}

// EvaluateAll runs all rules in priority order against an email.
// Returns the first non-skip result, or skip if no rules match.
func (e *Engine) EvaluateAll(rules []models.Rule, email *models.EmailContext) (*models.RuleResult, *models.Rule, error) {
	for i := range rules {
		rule := &rules[i]
		result, err := e.Evaluate(rule, email)
		if err != nil {
			log.Warn().Err(err).Str("rule", rule.Name).Msg("rule evaluation failed")
			continue
		}
		if result.Action != "skip" {
			return result, rule, nil
		}
	}
	return &models.RuleResult{Action: "skip"}, nil, nil
}

// ValidateLua checks if Lua code parses without executing it.
func (e *Engine) ValidateLua(code string) error {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()

	_, err := L.LoadString(code)
	if err != nil {
		return fmt.Errorf("syntax error: %w", err)
	}
	return nil
}

// TestRule evaluates a rule against a synthetic email context for testing.
func (e *Engine) TestRule(rule *models.Rule, email *models.EmailContext) (*models.RuleResult, error) {
	return e.Evaluate(rule, email)
}

// openSafeLibs opens only safe Lua standard libraries.
func openSafeLibs(L *lua.LState) {
	// Base library (print, type, tostring, etc.) but we'll override dangerous ones
	lua.OpenBase(L)
	lua.OpenTable(L)
	lua.OpenString(L)
	lua.OpenMath(L)

	// Remove dangerous base functions
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring"} {
		L.SetGlobal(name, lua.LNil)
	}
}

// registerHelpers adds utility functions to the Lua environment.
func registerHelpers(L *lua.LState) {
	// string.contains(haystack, needle) - case-insensitive
	L.SetGlobal("contains", L.NewFunction(func(L *lua.LState) int {
		haystack := strings.ToLower(L.CheckString(1))
		needle := strings.ToLower(L.CheckString(2))
		L.Push(lua.LBool(strings.Contains(haystack, needle)))
		return 1
	}))

	// string.contains_any(haystack, {needle1, needle2, ...}) - case-insensitive
	L.SetGlobal("contains_any", L.NewFunction(func(L *lua.LState) int {
		haystack := strings.ToLower(L.CheckString(1))
		tbl := L.CheckTable(2)
		found := false
		tbl.ForEach(func(_, v lua.LValue) {
			if strings.Contains(haystack, strings.ToLower(v.String())) {
				found = true
			}
		})
		L.Push(lua.LBool(found))
		return 1
	}))

	// matches(text, pattern) - Lua pattern match (case-insensitive)
	L.SetGlobal("matches", L.NewFunction(func(L *lua.LState) int {
		text := strings.ToLower(L.CheckString(1))
		pattern := strings.ToLower(L.CheckString(2))
		matched := strings.Contains(text, pattern)
		L.Push(lua.LBool(matched))
		return 1
	}))

	// ends_with(text, suffix) - case-insensitive
	L.SetGlobal("ends_with", L.NewFunction(func(L *lua.LState) int {
		text := strings.ToLower(L.CheckString(1))
		suffix := strings.ToLower(L.CheckString(2))
		L.Push(lua.LBool(strings.HasSuffix(text, suffix)))
		return 1
	}))

	// starts_with(text, prefix) - case-insensitive
	L.SetGlobal("starts_with", L.NewFunction(func(L *lua.LState) int {
		text := strings.ToLower(L.CheckString(1))
		prefix := strings.ToLower(L.CheckString(2))
		L.Push(lua.LBool(strings.HasPrefix(text, prefix)))
		return 1
	}))

	// domain_of(email_address) - extract domain from email
	L.SetGlobal("domain_of", L.NewFunction(func(L *lua.LState) int {
		addr := L.CheckString(1)
		parts := strings.SplitN(addr, "@", 2)
		if len(parts) == 2 {
			L.Push(lua.LString(strings.ToLower(parts[1])))
		} else {
			L.Push(lua.LString(strings.ToLower(addr)))
		}
		return 1
	}))

	// older_than(seconds) - check if email is older than N seconds
	L.SetGlobal("older_than", L.NewFunction(func(L *lua.LState) int {
		seconds := L.CheckNumber(1)
		// email.age_seconds is set in the context
		emailTbl := L.GetGlobal("email")
		if tbl, ok := emailTbl.(*lua.LTable); ok {
			age := tbl.RawGetString("age_seconds")
			if ageNum, ok := age.(lua.LNumber); ok {
				L.Push(lua.LBool(float64(ageNum) > float64(seconds)))
				return 1
			}
		}
		L.Push(lua.LBool(false))
		return 1
	}))

	// older_than_hours(hours)
	L.SetGlobal("older_than_hours", L.NewFunction(func(L *lua.LState) int {
		hours := L.CheckNumber(1)
		emailTbl := L.GetGlobal("email")
		if tbl, ok := emailTbl.(*lua.LTable); ok {
			age := tbl.RawGetString("age_seconds")
			if ageNum, ok := age.(lua.LNumber); ok {
				L.Push(lua.LBool(float64(ageNum) > float64(hours)*3600))
				return 1
			}
		}
		L.Push(lua.LBool(false))
		return 1
	}))

	// older_than_days(days)
	L.SetGlobal("older_than_days", L.NewFunction(func(L *lua.LState) int {
		days := L.CheckNumber(1)
		emailTbl := L.GetGlobal("email")
		if tbl, ok := emailTbl.(*lua.LTable); ok {
			age := tbl.RawGetString("age_seconds")
			if ageNum, ok := age.(lua.LNumber); ok {
				L.Push(lua.LBool(float64(ageNum) > float64(days)*86400))
				return 1
			}
		}
		L.Push(lua.LBool(false))
		return 1
	}))

	// has_attachment_type(content_type) - check if email has attachment of given type
	L.SetGlobal("has_attachment_type", L.NewFunction(func(L *lua.LState) int {
		contentType := strings.ToLower(L.CheckString(1))
		emailTbl := L.GetGlobal("email")
		if tbl, ok := emailTbl.(*lua.LTable); ok {
			types := tbl.RawGetString("attachment_types")
			if typesTbl, ok := types.(*lua.LTable); ok {
				found := false
				typesTbl.ForEach(func(_, v lua.LValue) {
					if strings.Contains(strings.ToLower(v.String()), contentType) {
						found = true
					}
				})
				L.Push(lua.LBool(found))
				return 1
			}
		}
		L.Push(lua.LBool(false))
		return 1
	}))

	// has_ics() - check if email has calendar attachment
	L.SetGlobal("has_ics", L.NewFunction(func(L *lua.LState) int {
		emailTbl := L.GetGlobal("email")
		if tbl, ok := emailTbl.(*lua.LTable); ok {
			names := tbl.RawGetString("attachment_names")
			types := tbl.RawGetString("attachment_types")
			if namesTbl, ok := names.(*lua.LTable); ok {
				found := false
				namesTbl.ForEach(func(_, v lua.LValue) {
					if strings.HasSuffix(strings.ToLower(v.String()), ".ics") {
						found = true
					}
				})
				if found {
					L.Push(lua.LTrue)
					return 1
				}
			}
			if typesTbl, ok := types.(*lua.LTable); ok {
				found := false
				typesTbl.ForEach(func(_, v lua.LValue) {
					if strings.Contains(strings.ToLower(v.String()), "calendar") {
						found = true
					}
				})
				L.Push(lua.LBool(found))
				return 1
			}
		}
		L.Push(lua.LBool(false))
		return 1
	}))

	// now_hour() - current hour in UTC
	L.SetGlobal("now_hour", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNumber(time.Now().UTC().Hour()))
		return 1
	}))

	// is_reply() - check if subject starts with Re:/Fwd:/etc.
	L.SetGlobal("is_reply", L.NewFunction(func(L *lua.LState) int {
		emailTbl := L.GetGlobal("email")
		if tbl, ok := emailTbl.(*lua.LTable); ok {
			subject := strings.ToLower(strings.TrimSpace(tbl.RawGetString("subject").String()))
			prefixes := []string{"re:", "fwd:", "fw:", "aw:", "sv:", "tr:"}
			for _, p := range prefixes {
				if strings.HasPrefix(subject, p) {
					L.Push(lua.LTrue)
					return 1
				}
			}
		}
		L.Push(lua.LFalse)
		return 1
	}))
}

// registerActions adds action functions that rules call to set their result.
func registerActions(L *lua.LState) {
	// skip() - rule doesn't apply
	L.SetGlobal("skip", L.NewFunction(func(L *lua.LState) int {
		setResult(L, "skip", "", 0, "")
		return 0
	}))

	// delete(reason?) - delete the message
	L.SetGlobal("delete", L.NewFunction(func(L *lua.LState) int {
		reason := L.OptString(1, "")
		setResult(L, "delete", "", 0, reason)
		return 0
	}))

	// archive(reason?) - archive the message
	L.SetGlobal("archive", L.NewFunction(func(L *lua.LState) int {
		reason := L.OptString(1, "")
		setResult(L, "archive", "", 0, reason)
		return 0
	}))

	// move(folder, reason?) - move to a specific folder
	L.SetGlobal("move", L.NewFunction(func(L *lua.LState) int {
		folder := L.CheckString(1)
		reason := L.OptString(2, "")
		setResult(L, "move", folder, 0, reason)
		return 0
	}))

	// keep(reason?) - explicitly keep, stop rule chain
	L.SetGlobal("keep", L.NewFunction(func(L *lua.LState) int {
		reason := L.OptString(1, "")
		setResult(L, "keep", "", 0, reason)
		return 0
	}))

	// notify(message, reason?) - send a notification
	L.SetGlobal("notify", L.NewFunction(func(L *lua.LState) int {
		message := L.CheckString(1)
		reason := L.OptString(2, "")
		setResult(L, "notify", message, 0, reason)
		return 0
	}))

	// defer_action(action, target, delay_seconds, reason?) - schedule for later
	L.SetGlobal("defer_action", L.NewFunction(func(L *lua.LState) int {
		action := L.CheckString(1)
		target := L.OptString(2, "")
		delay := L.CheckInt(3)
		reason := L.OptString(4, "")
		setResult(L, "defer", target, delay, reason)
		// Store the deferred action type in the result
		resultTbl := L.GetGlobal("__result").(*lua.LTable)
		resultTbl.RawSetString("deferred_action", lua.LString(action))
		return 0
	}))

	// move_after(folder, delay_seconds, reason?) - move after a delay
	L.SetGlobal("move_after", L.NewFunction(func(L *lua.LState) int {
		folder := L.CheckString(1)
		delay := L.CheckInt(2)
		reason := L.OptString(3, "")
		setResult(L, "defer", folder, delay, reason)
		resultTbl := L.GetGlobal("__result").(*lua.LTable)
		resultTbl.RawSetString("deferred_action", lua.LString("move"))
		return 0
	}))

	// delete_after(delay_seconds, reason?) - delete after a delay
	L.SetGlobal("delete_after", L.NewFunction(func(L *lua.LState) int {
		delay := L.CheckInt(1)
		reason := L.OptString(2, "")
		setResult(L, "defer", "", delay, reason)
		resultTbl := L.GetGlobal("__result").(*lua.LTable)
		resultTbl.RawSetString("deferred_action", lua.LString("delete"))
		return 0
	}))
}

func setResult(L *lua.LState, action, target string, delay int, reason string) {
	resultTbl := L.GetGlobal("__result").(*lua.LTable)
	resultTbl.RawSetString("action", lua.LString(action))
	resultTbl.RawSetString("target", lua.LString(target))
	resultTbl.RawSetString("delay", lua.LNumber(delay))
	resultTbl.RawSetString("reason", lua.LString(reason))
}

func extractResult(L *lua.LState) *models.RuleResult {
	resultTbl := L.GetGlobal("__result").(*lua.LTable)
	return &models.RuleResult{
		Action: resultTbl.RawGetString("action").String(),
		Target: resultTbl.RawGetString("target").String(),
		Delay:  int(lua.LVAsNumber(resultTbl.RawGetString("delay"))),
		Reason: resultTbl.RawGetString("reason").String(),
	}
}

func emailToLua(L *lua.LState, email *models.EmailContext) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("message_id", lua.LString(email.MessageID))
	tbl.RawSetString("subject", lua.LString(email.Subject))
	tbl.RawSetString("sender_name", lua.LString(email.SenderName))
	tbl.RawSetString("sender_address", lua.LString(email.SenderAddress))
	tbl.RawSetString("sender", lua.LString(email.SenderAddress)) // alias
	tbl.RawSetString("age_seconds", lua.LNumber(email.AgeSeconds))
	tbl.RawSetString("body_preview", lua.LString(email.BodyPreview))
	tbl.RawSetString("has_attachments", lua.LBool(email.HasAttachments))
	tbl.RawSetString("folder", lua.LString(email.Folder))

	// Recipients
	recipTbl := L.NewTable()
	for i, r := range email.Recipients {
		recipTbl.RawSetInt(i+1, lua.LString(r))
	}
	tbl.RawSetString("recipients", recipTbl)

	// Attachment names
	namesTbl := L.NewTable()
	for i, n := range email.AttachmentNames {
		namesTbl.RawSetInt(i+1, lua.LString(n))
	}
	tbl.RawSetString("attachment_names", namesTbl)

	// Attachment types
	typesTbl := L.NewTable()
	for i, t := range email.AttachmentTypes {
		typesTbl.RawSetInt(i+1, lua.LString(t))
	}
	tbl.RawSetString("attachment_types", typesTbl)

	// Headers
	headersTbl := L.NewTable()
	for k, v := range email.Headers {
		headersTbl.RawSetString(k, lua.LString(v))
	}
	tbl.RawSetString("headers", headersTbl)

	// Date as ISO string
	tbl.RawSetString("date", lua.LString(email.Date.Format(time.RFC3339)))

	return tbl
}
