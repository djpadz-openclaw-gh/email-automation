package imaplistener

import "strings"

// trashFolders lists folder names that should NOT trigger rule creation
// when a message is moved to them.
var trashFolders = map[string]bool{
	"trash":              true,
	"deleted messages":   true,
	"deleted items":      true,
	"[gmail]/trash":      true,
	"[gmail]/all mail":   true,
	"[gmail]/spam":       true,
	"junk":              true,
	"spam":              true,
	"junk e-mail":       true,
	"bulk mail":         true,
}

// isTrashFolder returns true if the folder name matches a known
// trash/junk/spam folder that should not trigger rule creation.
func isTrashFolder(folder string) bool {
	lower := strings.ToLower(folder)
	if trashFolders[lower] {
		return true
	}
	// Also check if folder starts with common trash prefixes
	if strings.HasPrefix(lower, "[gmail]/trash") {
		return true
	}
	if strings.HasPrefix(lower, "[gmail]/spam") {
		return true
	}
	return false
}
