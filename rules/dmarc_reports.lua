-- Rule: DMARC Reports
-- Move DMARC aggregate reports to @30DayTrash immediately.

local subject = email.subject:lower()

-- Strip common email client prefixes like [Preview], [Fwd:], etc.
subject = subject:gsub("^%s*%[[^%]]-]%s*", "")

local phrases = {
    "report domain:",
    "dmarc aggregate report",
    "dmarc feedback report",
    "dmarc report for",
}

local match = false
for _, phrase in ipairs(phrases) do
    if subject:find(phrase, 1, true) then
        match = true
        break
    end
end

if not match then return skip() end

return move("@30DayTrash", "DMARC aggregate report")
