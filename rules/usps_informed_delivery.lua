-- Rule: USPS
-- Move USPS emails older than 1 day to @30DayTrash.
-- Covers Informed Delivery digests and shipping/tracking notifications.

if not older_than_days(1) then return skip() end

local sender = email.sender_address:lower()

if sender == "informeddelivery@usps.gov" or
   sender == "informeddelivery@email.usps.gov" or
   sender == "auto-reply@usps.com" or
   ends_with(sender, "usps.gov") then
    return move("@30DayTrash", "USPS email, older than 1 day")
end

return skip()
