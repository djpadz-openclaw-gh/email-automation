-- Rule: IronPort Alerts
-- Move original IronPort alert emails to @Ironport.
-- Skip replies (Re:/Fwd:/etc.).

local subject = email.subject or ""
local sender = email.sender_address:lower()
local sender_name = (email.sender_name or ""):lower()

-- Skip replies
if is_reply() then return skip() end

-- Check sender patterns
local sender_patterns = {
    "alert@mx%d+%.ctb%.padz%.net",
    "alert@sma%.ctb%.padz%.net",
}

local subject_keywords = {
    "ironport",
    "spam quarantine",
    "cisco secure email",
    "email security appliance",
}

local sender_match = false
for _, pattern in ipairs(sender_patterns) do
    if sender:find(pattern) or sender_name:find(pattern) then
        sender_match = true
        break
    end
end

local subject_match = false
local subject_l = subject:lower()
for _, kw in ipairs(subject_keywords) do
    if subject_l:find(kw, 1, true) then
        subject_match = true
        break
    end
end

if not (sender_match or subject_match) then return skip() end

return move("@Ironport", "IronPort alert email")
