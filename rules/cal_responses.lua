-- Rule: Calendar Responses
-- Archive calendar accept/decline/tentative responses older than 24 hours.
-- Only acts on messages that have a .ics attachment.

if not older_than_hours(24) then return skip() end
if not has_ics() then return skip() end

if starts_with(email.subject, "Accepted:") or
   starts_with(email.subject, "Declined:") or
   starts_with(email.subject, "Tentative:") then
    return archive("Calendar response with .ics attachment, older than 24h")
end

return skip()
