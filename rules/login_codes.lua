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
if contains_any(subject, code_phrases) then
    return delete("Login/verification code email, older than 1h")
end

-- Trusted auth sender + auth keyword match
local trusted_domains = {
    "anthropic.com", "accounts.google.com", "account.microsoft.com",
    "github.com", "okta.com", "auth0.com", "onelogin.com", "atlassian.com",
}

local auth_keywords = {
    "sign in", "sign-in", "log in", "login", "verify",
    "verification", "authenticate", "access your account",
}

local is_trusted = false
for _, domain in ipairs(trusted_domains) do
    if sender:match(domain:gsub("%.", "%%.")) then
        is_trusted = true
        break
    end
end

if is_trusted and contains_any(subject, auth_keywords) then
    return delete("Auth email from trusted sender, older than 1h")
end

return skip()
