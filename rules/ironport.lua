-- Rule: IronPort Alerts
-- Move original IronPort alert emails to @Ironport.
-- Skip replies (Re:/Fwd:/etc.).

if is_reply() then return skip() end

local sender = email.sender_address:lower()
local sender_name = (email.sender_name or ""):lower()
local subject = email.subject:lower()

-- Check sender patterns (Lua patterns, not plain text)
local sender_match = sender:match("alert@mx%d+%.ctb%.padz%.net") or
                     sender:match("alert@sma%.ctb%.padz%.net") or
                     sender_name:match("alert@mx%d+%.ctb%.padz%.net") or
                     sender_name:match("alert@sma%.ctb%.padz%.net")

local subject_match = contains_any(subject, {
    "ironport", "spam quarantine", "cisco secure email", "email security appliance",
})

if not (sender_match or subject_match) then return skip() end

return move("@Ironport", "IronPort alert email")
