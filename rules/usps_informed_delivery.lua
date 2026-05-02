-- Rule: USPS
-- Move USPS emails older than 1 day to @30DayTrash.
-- Covers Informed Delivery digests and shipping/tracking notifications.

if not older_than_days(1) then return skip() end

local sender = email.sender_address:lower()

local usps_senders = {
    "informeddelivery@usps.gov",
    "informeddelivery@email.usps.gov",
    "auto-reply@usps.com",
}

local usps_domains = {
    "usps.gov",
    "email.informeddelivery.usps.gov",
}

local is_usps = false

for _, s in ipairs(usps_senders) do
    if sender == s then
        is_usps = true
        break
    end
end

if not is_usps then
    for _, d in ipairs(usps_domains) do
        if ends_with(sender, d) then
            is_usps = true
            break
        end
    end
end

if not is_usps then return skip() end

return move("@30DayTrash", "USPS email, older than 1 day")
