-- Rule: Morgan Stanley
-- Move Morgan Stanley notification emails older than 24 hours to @MorganStanley.

local sender = email.sender_address:lower()
local subject = email.subject:lower()

if not sender:match("morganstanley%.com") then return skip() end

local keywords = {
    "mobile check deposit", "deposit", "transfer", "statement",
    "transaction", "account alert", "account notice", "payment",
    "withdrawal", "confirmation",
}

if not contains_any(subject, keywords) then return skip() end

if not older_than_hours(24) then
    return keep("Morgan Stanley notification, keeping until 24h old")
end

return move("@MorganStanley", "Morgan Stanley notification filed")
