-- Rule: Teams Notifications
-- Delete Microsoft Teams notification emails older than 6 hours.

if not older_than_hours(6) then return skip() end

local sender = email.sender_address:lower()

local teams_senders = {
    "no-reply@teams.microsoft.com",
    "noreply@teams.microsoft.com",
}

for _, s in ipairs(teams_senders) do
    if sender == s then
        return delete("Teams notification, older than 6h")
    end
end

return skip()
