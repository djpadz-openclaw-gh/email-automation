-- Rule: Contabo
-- Move Contabo billing emails to @Receipts.

local sender = email.sender_address:lower()
local subject = email.subject:lower()

if not sender:find("contabo.com", 1, true) then return skip() end

if not (subject:find("credit card payment", 1, true) or subject:find("invoice", 1, true)) then
    return skip()
end

return move("@Receipts", "Contabo billing email")
