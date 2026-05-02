-- Rule: SaneBox
-- Delete SaneBox daily digest/status emails older than 24 hours.

if not older_than_hours(24) then return skip() end

local sender = email.sender_address:lower()
local subject = email.subject:lower()

-- Match by sender domain
if sender:find("sanebox.com", 1, true) then
    return delete("SaneBox digest, older than 24h")
end

-- Match by subject phrases
local phrases = {
    "sanebox", "your sane", "sane digest", "sane black hole",
    "sanelater", "sane inbox", "emails you've missed", "digest from sanebox",
}

for _, phrase in ipairs(phrases) do
    if subject:find(phrase, 1, true) then
        return delete("SaneBox digest (subject match), older than 24h")
    end
end

return skip()
