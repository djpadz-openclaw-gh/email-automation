-- Rule: Scripps Video Visit
-- Delete Scripps Health video visit direct join link emails after 24 hours.
-- These are one-time-use telehealth join links.

if not older_than_hours(24) then return skip() end

local sender = email.sender_address:lower()

if sender ~= "myscrippsdonotreply@myscripps.org" then return skip() end

if contains_any(email.subject:lower(), {
    "video visit direct join",
    "video visit join",
    "direct join link",
}) then
    return delete("Scripps video visit link, older than 24h")
end

return skip()
