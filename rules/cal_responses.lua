-- Rule: Calendar Responses
-- Archive calendar accept/decline/tentative responses older than 24 hours.
-- Only acts on messages that have a .ics attachment.

local subject = email.subject

if not older_than_hours(24) then return skip() end

-- Check for response prefixes (case-sensitive, as in original)
local prefixes = { "Accepted:", "Declined:", "Tentative:" }
local prefix_match = false
for _, prefix in ipairs(prefixes) do
    if subject:sub(1, #prefix) == prefix then
        prefix_match = true
        break
    end
end

if not prefix_match then return skip() end

-- Check for .ics attachment
if not has_ics() then return skip() end

return archive("Calendar response with .ics attachment, older than 24h")
