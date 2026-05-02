-- Rule: Amazon
-- Move Amazon order/shipping emails older than 24 hours to @Amazon.
-- Young Amazon emails (< 24h) return KEEP to prevent the receipts rule from grabbing them.

local sender = email.sender_address:lower()
local subject = email.subject:lower()

local domains = {
    "amazon.com", "amazon.co.uk", "amazon.ca", "amazon.de",
    "amazon.fr", "amazon.co.jp", "amazon.com.au", "marketplace.amazon.com",
}

local keywords = {
    "your order", "order #", "order number", "has shipped", "order shipped",
    "out for delivery", "delivered", "your package", "your shipment",
    "arriving", "delivery", "confirm your order", "order confirmation",
    "order update", "review your upcoming", "subscribe & save",
}

-- Check sender domain
local sender_match = false
for _, domain in ipairs(domains) do
    if sender:find(domain, 1, true) then
        sender_match = true
        break
    end
end

if not sender_match then return skip() end

-- Check subject keywords
if not contains_any(subject, keywords) then return skip() end

-- Hold in inbox until 24h old, then file
if not older_than_hours(24) then
    return keep("Amazon order email, keeping until 24h old")
end

return move("@Amazon", "Amazon order/shipping email filed")
