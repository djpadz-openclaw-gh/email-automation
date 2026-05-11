-- Rule: Contabo
-- Move Contabo billing emails to @Receipts.

local sender = email.sender_address:lower()
local subject = email.subject:lower()

if not sender:match("contabo%.com") then return skip() end

if not (subject:match("credit card payment") or subject:match("invoice")) then
    return skip()
end

return move("@Receipts", "Contabo billing email")
