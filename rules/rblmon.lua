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

for _, phrase in ipairs(no_block_phrases) do
    if subject:find(phrase, 1, true) or preview:find(phrase, 1, true) then
        return delete("rblmon all-clear notification")
    end
end

return skip()
