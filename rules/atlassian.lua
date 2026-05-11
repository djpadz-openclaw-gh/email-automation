-- Rule: Atlassian
-- Move Atlassian payment confirmation emails to @Receipts & Invoices.

local sender = email.sender_address:lower()
local subject = email.subject:lower()

if not sender:match("atlassian%.com") then return skip() end
if not subject:match("your payment has been processed") then return skip() end

return move("@Receipts & Invoices", "Atlassian payment confirmation")
