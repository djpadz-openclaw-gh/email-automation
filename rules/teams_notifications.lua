-- Rule: Teams Notifications
-- Delete Microsoft Teams notification emails older than 6 hours.

if not older_than_hours(6) then return skip() end

local sender = email.sender_address:lower()

if sender == "no-reply@teams.microsoft.com" or
   sender == "noreply@teams.microsoft.com" then
    return delete("Teams notification, older than 6h")
end

return skip()
