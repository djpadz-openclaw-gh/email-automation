-- Rule: rblmon
-- Delete rblmon.com "no blocks found" notification emails.
-- Only deletes all-clear messages; keeps actual block alerts.

local sender = email.sender_address:lower()

if sender ~= "alert@rblmon.com" then return skip() end

local subject = email.subject:lower()
local preview = email.body_preview:lower()

local no_block_phrases = {
    "no blocks", "no blacklist", "not listed", "not blocked",
    "all clear", "clean", "0 blacklists", "0 blocks", "found on 0",
}

if contains_any(subject, no_block_phrases) or
   contains_any(preview, no_block_phrases) then
    return delete("rblmon all-clear notification")
end

return skip()
