-- Rule: CrowdStrike
-- Move CrowdStrike Intelligence Weekly Reports to @SaneNews.

local sender = email.sender_address:lower()
local subject = email.subject  -- case-sensitive check for "CSWR"

if not sender:find("crowdstrike.com", 1, true) then return skip() end
if not subject:find("CSWR", 1, true) then return skip() end

return move("@SaneNews", "CrowdStrike weekly report")
