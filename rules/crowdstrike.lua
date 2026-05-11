-- Rule: CrowdStrike
-- Move CrowdStrike Intelligence Weekly Reports to @SaneNews.

local sender = email.sender_address:lower()

if not sender:match("crowdstrike%.com") then return skip() end
if not email.subject:match("CSWR") then return skip() end

return move("@SaneNews", "CrowdStrike weekly report")
