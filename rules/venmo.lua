-- Rule: Venmo
-- Move Venmo "you paid" notification emails older than 2 days to @Receipts.

if not older_than_days(2) then return skip() end

local sender = email.sender_address:lower()

if not (sender == "venmo@venmo.com" or ends_with(sender, "@venmo.com")) then
    return skip()
end

if not email.subject:lower():match("you paid") then return skip() end

return move("@Receipts", "Venmo payment notification, older than 2 days")
