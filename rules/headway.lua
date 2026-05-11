-- Rule: Headway
-- Delete Headway appointment reminder emails after the appointment day has passed.
-- Falls back to deleting after 2 days if date can't be parsed.

local sender = email.sender_address:lower()

if not sender:match("headway%.co") then return skip() end

-- Try to parse appointment date from subject (e.g., "on 3/24" or "on 12/5/2025")
local month, day = email.subject:match("[Oo]n%s+(%d+)/(%d+)")

if month and day then
    -- We have a date; the engine doesn't have full date parsing,
    -- so we use a conservative 2-day fallback
    if older_than_days(2) then
        return delete("Headway reminder, appointment date passed")
    end
else
    if older_than_days(2) then
        return delete("Headway reminder, older than 2 days (no date parsed)")
    end
end

return skip()
