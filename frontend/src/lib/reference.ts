export const EXAMPLE_RULES = [
  {
    name: 'Simple sender match',
    description: 'Move emails from a specific sender to a folder',
    code: `-- Move emails from notifications@example.com to @Notifications
local sender = email.sender_address:lower()

if sender == "notifications@example.com" then
    return move("@Notifications", "Example notification")
end

return skip()`,
  },
  {
    name: 'Age-based cleanup',
    description: 'Delete old verification emails',
    code: `-- Delete verification code emails older than 2 hours
if not older_than_hours(2) then return skip() end

local subject = email.subject:lower()

if subject:match("verification code") or
   subject:match("login code") or
   subject:match("one%-time password") then
    return delete("Expired verification code")
end

return skip()`,
  },
  {
    name: 'Domain + keyword match',
    description: 'Match by sender domain AND subject keyword',
    code: `-- File billing emails from specific domains
local sender = email.sender_address:lower()
local subject = email.subject:lower()

if not (sender:match("stripe%.com") or
        sender:match("paypal%.com") or
        sender:match("square%.com")) then
    return skip()
end

if contains_any(subject, { "receipt", "invoice", "payment", "billing" }) then
    return move("@Receipts", "Billing email from " .. sender)
end

return skip()`,
  },
  {
    name: 'Calendar attachment filter',
    description: 'Archive old calendar responses with .ics attachments',
    code: `-- Archive calendar responses older than 24h that have .ics attachments
if not older_than_hours(24) then return skip() end
if not has_ics() then return skip() end

if starts_with(email.subject, "Accepted:") or
   starts_with(email.subject, "Declined:") or
   starts_with(email.subject, "Tentative:") then
    return archive("Old calendar response")
end

return skip()`,
  },
  {
    name: 'Notification with template',
    description: 'Send a Telegram notification for important emails',
    code: `-- Notify on emails from the CEO
local sender = email.sender_address:lower()

if sender == "ceo@company.com" then
    return notify(
        "\xF0\x9F\x94\x94 Email from CEO: " .. email.subject,
        "Priority sender notification"
    )
end

return skip()`,
  },
];

export const REFERENCE_DOCS = {
  emailContext: {
    title: 'Email Context',
    description: 'The `email` table is available in every rule and contains the message data.',
    fields: [
      { name: 'email.message_id', type: 'string', description: 'Unique message identifier' },
      { name: 'email.subject', type: 'string', description: 'Email subject line' },
      { name: 'email.sender_name', type: 'string', description: 'Sender display name' },
      { name: 'email.sender_address', type: 'string', description: 'Sender email address' },
      { name: 'email.sender', type: 'string', description: 'Alias for sender_address' },
      { name: 'email.recipients', type: 'table', description: 'List of recipient addresses' },
      { name: 'email.date', type: 'string', description: 'ISO 8601 date string' },
      { name: 'email.age_seconds', type: 'number', description: 'Age of message in seconds' },
      { name: 'email.body_preview', type: 'string', description: 'First ~200 chars of body text' },
      { name: 'email.has_attachments', type: 'boolean', description: 'Whether message has attachments' },
      { name: 'email.attachment_names', type: 'table', description: 'List of attachment filenames' },
      { name: 'email.attachment_types', type: 'table', description: 'List of attachment MIME types' },
      { name: 'email.headers', type: 'table', description: 'Key-value map of email headers' },
      { name: 'email.folder', type: 'string', description: 'Current IMAP folder name' },
    ],
  },
  actions: {
    title: 'Action Functions',
    description: 'Call one of these to set the rule result. Only the last action called takes effect.',
    functions: [
      { name: 'skip()', description: 'Rule does not apply — try the next rule' },
      { name: 'delete(reason?)', description: 'Permanently delete the message' },
      { name: 'archive(reason?)', description: 'Move to archive folder' },
      { name: 'move(folder, reason?)', description: 'Move to a specific IMAP folder' },
      { name: 'keep(reason?)', description: 'Explicitly keep in inbox, stop rule chain' },
      { name: 'notify(message, reason?)', description: 'Send a Telegram notification' },
      { name: 'flag(flag_name, reason?)', description: 'Set an IMAP flag (Junk, Flagged, Seen, Answered, Draft, Deleted)' },
      { name: 'defer_action(action, target, delay_secs, reason?)', description: 'Schedule an action for later' },
      { name: 'move_after(folder, delay_secs, reason?)', description: 'Move to folder after delay' },
      { name: 'delete_after(delay_secs, reason?)', description: 'Delete after delay' },
    ],
  },
  helpers: {
    title: 'Helper Functions',
    description: 'Utility functions available in the Lua sandbox.',
    functions: [
      { name: 'contains(haystack, needle)', description: 'Case-insensitive string contains' },
      { name: 'contains_any(haystack, {needles})', description: 'Case-insensitive check against list' },
      { name: 'starts_with(text, prefix)', description: 'Case-insensitive prefix check' },
      { name: 'ends_with(text, suffix)', description: 'Case-insensitive suffix check' },
      { name: 'matches(text, pattern)', description: 'Lua pattern match (case-insensitive)' },
      { name: 'domain_of(email_addr)', description: 'Extract domain from email address' },
      { name: 'older_than(seconds)', description: 'Check if email is older than N seconds' },
      { name: 'older_than_hours(hours)', description: 'Check if email is older than N hours' },
      { name: 'older_than_days(days)', description: 'Check if email is older than N days' },
      { name: 'has_ics()', description: 'Check if email has .ics calendar attachment' },
      { name: 'has_attachment_type(mime_type)', description: 'Check for attachment by MIME type' },
      { name: 'is_reply()', description: 'Check if subject starts with Re:/Fwd:/etc.' },
      { name: 'now_hour()', description: 'Current hour in UTC (0-23)' },
    ],
  },
  luaStdlib: {
    title: 'Lua Standard Library (Safe Subset)',
    description: 'The following Lua standard library modules are available. Filesystem, network, and OS access are disabled.',
    modules: [
      { name: 'Base', items: 'print, type, tostring, tonumber, pairs, ipairs, next, select, unpack, error, pcall, xpcall, assert, rawget, rawset, rawequal, rawlen, setmetatable, getmetatable' },
      { name: 'string', items: 'string.byte, string.char, string.find, string.format, string.gmatch, string.gsub, string.len, string.lower, string.match, string.rep, string.reverse, string.sub, string.upper' },
      { name: 'table', items: 'table.concat, table.insert, table.remove, table.sort, table.unpack' },
      { name: 'math', items: 'math.abs, math.ceil, math.floor, math.max, math.min, math.random, math.huge' },
    ],
  },
};
