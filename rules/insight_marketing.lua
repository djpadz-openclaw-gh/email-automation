-- Rule: Insight Marketing
-- Move marketing emails from insight.com to @SaneNews.

local sender = email.sender_address:lower()

local senders = { "mktg.insight.com", "insight.com" }

for _, s in ipairs(senders) do
    if sender:find(s, 1, true) then
        return move("@SaneNews", "Insight marketing email")
    end
end

return skip()
