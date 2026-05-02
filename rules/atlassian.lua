-- Rule: Atlassian
-- Move Atlassian payment confirmation emails to @Receipts & Invoices.

local sender = email.sender_address:lower()
local subject = email.subject:lower()

if not sender:find("atlassian.com", 1, true) then return skip() end
if not subject:find("your payment has been processed", 1, true) then return skip() end

return move("@Receipts & Invoices", "Atlassian payment confirmation")
