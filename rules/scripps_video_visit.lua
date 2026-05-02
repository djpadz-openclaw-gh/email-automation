-- Rule: Scripps Video Visit
-- Delete Scripps Health video visit direct join link emails after 24 hours.
-- These are one-time-use telehealth join links.

if not older_than_hours(24) then return skip() end

local sender = email.sender_address:lower()

local scripps_senders = {
    "myscrippsdonotreply@myscripps.org",
}

local is_scripps = false
for _, s in ipairs(scripps_senders) do
    if sender == s then
        is_scripps = true
        break
    end
end

if not is_scripps then return skip() end

local subject = email.subject:lower()
local phrases = {
    "video visit direct join",
    "video visit join",
    "direct join link",
}

for _, phrase in ipairs(phrases) do
    if subject:find(phrase, 1, true) then
        return delete("Scripps video visit link, older than 24h")
    end
end

return skip()
