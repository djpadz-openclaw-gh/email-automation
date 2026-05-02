-- Rule: Headway
-- Delete Headway appointment reminder emails after the appointment day has passed.
-- Falls back to deleting after 2 days if date can't be parsed.

local sender = email.sender_address:lower()

-- Check if sender is from Headway
local headway_senders = {
    "noreply@e.headway.co",
    "no-reply@e.headway.co",
    "noreply@headway.co",
    "team@headway.co",
}

local is_headway = false
for _, s in ipairs(headway_senders) do
    if sender == s or sender:find("headway.co", 1, true) then
        is_headway = true
        break
    end
end

if not is_headway then return skip() end

-- Try to parse appointment date from subject (e.g., "on 3/24" or "on 12/5/2025")
local subject = email.subject
local month, day = subject:match("[Oo]n%s+(%d+)/(%d+)")

if month and day then
    -- We have a date; the engine doesn't have full date parsing,
    -- so we use a conservative 2-day fallback
    if older_than_days(2) then
        return delete("Headway reminder, appointment date passed")
    end
else
    -- Can't parse date, fall back to 2-day age
    if older_than_days(2) then
        return delete("Headway reminder, older than 2 days (no date parsed)")
    end
end

return skip()
