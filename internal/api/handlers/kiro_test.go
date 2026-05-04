package handlers

import "testing"

func TestStripMarkdownCodeBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no code block",
			input:    "-- Rule: test\nlocal x = 1",
			expected: "-- Rule: test\nlocal x = 1",
		},
		{
			name:     "lua code block",
			input:    "```lua\n-- Rule: test\nlocal x = 1\n```",
			expected: "-- Rule: test\nlocal x = 1",
		},
		{
			name:     "plain code block",
			input:    "```\n-- Rule: test\nlocal x = 1\n```",
			expected: "-- Rule: test\nlocal x = 1",
		},
		{
			name:     "code block with surrounding whitespace",
			input:    "  ```lua\n-- Rule: test\nlocal x = 1\n```  ",
			expected: "-- Rule: test\nlocal x = 1",
		},
		{
			name:     "opening fence only",
			input:    "```lua\n-- Rule: test\nlocal x = 1",
			expected: "-- Rule: test\nlocal x = 1",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   \n  ",
			expected: "",
		},
		{
			name:     "single line no fence",
			input:    "local x = 1",
			expected: "local x = 1",
		},
		{
			name:     "multiline lua in fences with extra blank lines",
			input:    "```lua\n\n-- Comment\nif true then\n  delete()\nend\n\n```",
			expected: "-- Comment\nif true then\n  delete()\nend",
		},
		{
			name:     "code block with language tag Lua (capitalized)",
			input:    "```Lua\nlocal x = 1\n```",
			expected: "local x = 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMarkdownCodeBlock(tt.input)
			if got != tt.expected {
				t.Errorf("stripMarkdownCodeBlock(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLooksLikeLua(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"comment", "-- Rule: test", true},
		{"local", "local x = 1", true},
		{"if", "if true then", true},
		{"function", "function foo()", true},
		{"return", "return skip()", true},
		{"empty", "", false},
		{"english text", "Move emails from Amazon to the archive folder", false},
		{"for loop", "for i = 1, 10 do", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeLua(tt.input)
			if got != tt.expected {
				t.Errorf("looksLikeLua(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
