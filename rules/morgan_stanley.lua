-- Rule: Morgan Stanley
-- Move Morgan Stanley notification emails older than 24 hours to @MorganStanley.

local sender = email.sender_address:lower()
local subject = email.subject:lower()

if not sender:find("morganstanley.com", 1, true) then return skip() end

local keywords = {
    "mobile check deposit", "deposit", "transfer", "statement",
    "transaction", "account alert", "account notice", "payment",
    "withdrawal", "confirmation",
}

local match = false
for _, kw in ipairs(keywords) do
    if subject:find(kw, 1, true) then
        match = true
        break
    end
end

if not match then return skip() end

if not older_than_hours(24) then
    return keep("Morgan Stanley notification, keeping until 24h old")
end

return move("@MorganStanley", "Morgan Stanley notification filed")
