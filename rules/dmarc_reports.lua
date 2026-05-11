-- Rule: DMARC Reports
-- Move DMARC aggregate reports to @30DayTrash immediately.

local subject = email.subject:lower()

-- Strip common email client prefixes like [Preview], [Fwd:], etc.
subject = subject:gsub("^%s*%[[^%]]-]%s*", "")

if contains_any(subject, {
    "report domain:",
    "dmarc aggregate report",
    "dmarc feedback report",
    "dmarc report for",
}) then
    return move("@30DayTrash", "DMARC aggregate report")
end

return skip()
