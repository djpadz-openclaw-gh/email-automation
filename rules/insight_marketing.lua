-- Rule: Insight Marketing
-- Move marketing emails from insight.com to @SaneNews.

local sender = email.sender_address:lower()

if sender:match("insight%.com") then
    return move("@SaneNews", "Insight marketing email")
end

return skip()
