-- Rule: Venmo
-- Move Venmo "you paid" notification emails older than 2 days to @Receipts.

if not older_than_days(2) then return skip() end

local sender = email.sender_address:lower()
local subject = email.subject:lower()

local venmo_senders = {
    "venmo@venmo.com",
    "no-reply@venmo.com",
    "noreply@venmo.com",
}

local is_venmo = false
for _, s in ipairs(venmo_senders) do
    if sender == s then
        is_venmo = true
        break
    end
end

if not is_venmo then
    if ends_with(sender, "@venmo.com") then
        is_venmo = true
    end
end

if not is_venmo then return skip() end

if not subject:find("you paid", 1, true) then return skip() end

return move("@Receipts", "Venmo payment notification, older than 2 days")
