package movedetect

import (
	"strings"
	"testing"
)

func TestIsComplexSender(t *testing.T) {
	tests := []struct {
		sender   string
		expected bool
	}{
		// Simple senders
		{"user@example.com", false},
		{"newsletter@company.com", false},
		{"support@github.com", false},
		{"noreply@amazon.com", false},

		// Complex senders
		{"bounce-djpadz=padz.net@mail.example.com", true},           // embedded email
		{"bounce-abc123-def456@example.com", true},                   // bounce prefix + hyphens
		{"bounces-12345-abcdef@sendgrid.net", true},                  // bounces prefix
		{"0102018a1b2c3d4e-f5a6b7c8@us-east-1.amazonses.com", true}, // hex-heavy
		{"a1b2c3d4-e5f6-7890-abcd-ef1234567890@bounce.example.com", true}, // UUID-like
		{"noreply+abc123def456@notifications.example.com", true},     // noreply with random suffix
		{"", true}, // empty sender

		// Edge cases
		{"user-name@example.com", false},       // single hyphen is fine
		{"first-last-name@example.com", false}, // two hyphens is fine
	}

	for _, tt := range tests {
		t.Run(tt.sender, func(t *testing.T) {
			result := isComplexSender(tt.sender)
			if result != tt.expected {
				t.Errorf("isComplexSender(%q) = %v, want %v", tt.sender, result, tt.expected)
			}
		})
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		subject  string
		expected []string
	}{
		{
			"Your Amazon order has shipped",
			[]string{"amazon", "order", "shipped"},
		},
		{
			"Re: Weekly team standup notes",
			[]string{"weekly", "team", "standup"},
		},
		{
			"[EXTERNAL] Important security update",
			[]string{"important", "security", "update"},
		},
		{
			"Fwd: Invoice #12345 from Acme Corp",
			[]string{"invoice", "acme", "corp"},
		},
		{
			"A B C", // all too short
			[]string{},
		},
		{
			"The quick brown fox jumps over the lazy dog",
			[]string{"quick", "brown", "fox"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			result := extractKeywords(tt.subject)
			if len(result) != len(tt.expected) {
				t.Errorf("extractKeywords(%q) = %v (len %d), want %v (len %d)",
					tt.subject, result, len(result), tt.expected, len(tt.expected))
				return
			}
			for i, kw := range result {
				if kw != tt.expected[i] {
					t.Errorf("extractKeywords(%q)[%d] = %q, want %q",
						tt.subject, i, kw, tt.expected[i])
				}
			}
		})
	}
}

func TestInferRule_SimpleSender(t *testing.T) {
	rule := InferRule("newsletter@company.com", "Weekly digest", "Newsletters")

	if !strings.Contains(rule, `email.sender == "newsletter@company.com"`) {
		t.Errorf("expected sender match in rule, got:\n%s", rule)
	}
	if !strings.Contains(rule, `move("Newsletters")`) {
		t.Errorf("expected move to Newsletters in rule, got:\n%s", rule)
	}
}

func TestInferRule_ComplexSender(t *testing.T) {
	rule := InferRule(
		"bounce-abc123-def456-ghi789@sendgrid.net",
		"Your weekly GitHub digest",
		"GitHub",
	)

	// Should NOT contain the complex sender address
	if strings.Contains(rule, "bounce-abc123") {
		t.Errorf("expected subject-based rule for complex sender, got:\n%s", rule)
	}
	// Should contain subject keywords
	if !strings.Contains(rule, `contains(email.subject,`) {
		t.Errorf("expected contains() call in rule, got:\n%s", rule)
	}
	if !strings.Contains(rule, `move("GitHub")`) {
		t.Errorf("expected move to GitHub in rule, got:\n%s", rule)
	}
}

func TestInferRule_ComplexSenderNoKeywords(t *testing.T) {
	rule := InferRule(
		"bounce-abc123-def456-ghi789@sendgrid.net",
		"Hi",
		"Archive",
	)

	// Should produce a disabled rule since no keywords can be extracted
	if !strings.Contains(rule, "if false then") {
		t.Errorf("expected disabled rule for no keywords, got:\n%s", rule)
	}
}

func TestInferRule_EscapesQuotes(t *testing.T) {
	rule := InferRule(`user"test@example.com`, "Normal subject here", "Folder")

	// Should escape the quote
	if strings.Contains(rule, `""`) && !strings.Contains(rule, `\"`) {
		t.Errorf("expected escaped quote in rule, got:\n%s", rule)
	}
}
