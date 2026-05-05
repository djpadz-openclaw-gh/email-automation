package imaplistener

import (
	"strings"

	"github.com/emersion/go-imap/v2"
)

// skipFolderAttrs are IMAP special-use attributes that indicate system folders
// where moves should NOT trigger rule creation.
var skipFolderAttrs = map[imap.MailboxAttr]bool{
	imap.MailboxAttrTrash:   true, // \Trash
	imap.MailboxAttrJunk:    true, // \Junk (spam)
	imap.MailboxAttrAll:     true, // \All (Gmail's All Mail)
	imap.MailboxAttrArchive: true, // \Archive
}

// systemTrashNames is a fallback list of exact folder names (case-insensitive)
// that are known system trash/junk/spam folders. Used when the IMAP server
// doesn't report special-use attributes (RFC 6154).
// NOTE: Only exact matches — NOT substring matching. This ensures user-created
// folders like @3DayTrash are not incorrectly filtered.
var systemTrashNames = map[string]bool{
	"trash":            true,
	"deleted messages": true,
	"deleted items":    true,
	"[gmail]/trash":    true,
	"[gmail]/all mail": true,
	"[gmail]/spam":     true,
	"junk":             true,
	"spam":             true,
	"junk e-mail":      true,
	"bulk mail":        true,
}

// isSkipFolder returns true if the folder should not trigger rule creation.
// It checks IMAP special-use attributes first (authoritative), then falls back
// to exact name matching for servers that don't report attributes.
// User-created folders like @3DayTrash will NOT match because we use exact
// name comparison, not substring matching.
func isSkipFolder(name string, attrs []imap.MailboxAttr) bool {
	// First: check IMAP attributes (authoritative if present)
	for _, attr := range attrs {
		if skipFolderAttrs[attr] {
			return true
		}
	}

	// Fallback: exact name match for well-known system folder names
	lower := strings.ToLower(name)
	return systemTrashNames[lower]
}
