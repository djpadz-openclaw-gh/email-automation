-- Rule: Login Codes
-- Delete temporary login/verification code emails older than 1 hour.

if not older_than_hours(1) then return skip() end

local subject = email.subject:lower()
local sender = email.sender_address:lower()

local code_phrases = {
    "verification code", "verify your email", "verify your account",
    "your otp", "one-time password", "one-time code", "2fa code",
    "two-factor", "magic link", "sign-in code", "login code",
    "your code is", "your pin is", "temporary password",
    "reset your password", "password reset", "secure link to log in",
    "click to sign in", "click here to log in", "verifying it's you",
    "verifying its you", "confirm your email address", "confirm your identity",
    "confirm it's you", "confirm its you",
}

-- Direct subject match
for _, phrase in ipairs(code_phrases) do
    if subject:find(phrase, 1, true) then
        return delete("Login/verification code email, older than 1h")
    end
end

-- Trusted auth sender + auth keyword match
local trusted_senders = {
    "anthropic.com", "accounts.google.com", "account.microsoft.com",
    "github.com", "okta.com", "auth0.com", "onelogin.com", "atlassian.com",
}

local auth_keywords = {
    "sign in", "sign-in", "log in", "login", "verify",
    "verification", "authenticate", "access your account",
}

local is_trusted = false
for _, domain in ipairs(trusted_senders) do
    if sender:find(domain, 1, true) then
        is_trusted = true
        break
    end
end

if is_trusted then
    for _, kw in ipairs(auth_keywords) do
        if subject:find(kw, 1, true) then
            return delete("Auth email from trusted sender, older than 1h")
        end
    end
end

return skip()
