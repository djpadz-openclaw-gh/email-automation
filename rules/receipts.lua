-- Rule: Receipts
-- Move receipt/order confirmation emails to @Receipts.
-- Uses subject keyword matching.

local subject = email.subject:lower()
local sender = email.sender_address:lower()

-- Strong subject phrases that indicate a receipt regardless of sender
local receipt_phrases = {
    "order confirmation", "order confirmed", "your order", "order #", "order number",
    "purchase confirmation", "purchase receipt", "payment confirmation", "payment receipt",
    "your receipt", "your invoice", "invoice #", "invoice number", "tax invoice",
    "your subscription confirmation", "your subscription receipt",
    "subscription confirmation", "subscription receipt", "subscription renewed",
    "renewal confirmation", "billing confirmation", "billing receipt",
    "transaction receipt", "has shipped", "your shipment", "shipment confirmation",
    "order shipped", "order has shipped", "your package", "delivery confirmation",
    "order delivered", "package delivered", "your download", "download receipt",
    "refund confirmation", "refund processed", "charge of",
    "you've been charged", "you have been charged",
    "payment successful", "payment was successful",
    "automatic payment", "autopayment", "invoice paid",
    "thank you for your payment", "auto reload is complete", "card auto reload",
}

if contains_any(subject, receipt_phrases) then
    return move("@Receipts", "Receipt detected by subject keyword")
end

-- Domain + subject keyword pairs (both must match)
local domain_pairs = {
    { "amazon%.com",       { "your order", "order #", "has shipped", "delivered", "invoice" } },
    { "apple%.com",        { "your receipt", "invoice", "order" } },
    { "paypal%.com",       { "receipt", "payment", "invoice", "transaction" } },
    { "stripe%.com",       { "receipt", "invoice", "payment" } },
    { "shopify%.com",      { "order", "receipt", "confirmation" } },
    { "etsy%.com",         { "order", "receipt" } },
    { "ebay%.com",         { "order", "invoice", "payment" } },
    { "walmart%.com",      { "order", "receipt", "confirmation" } },
    { "target%.com",       { "order", "receipt", "confirmation" } },
    { "bestbuy%.com",      { "order", "receipt", "confirmation" } },
    { "costco%.com",       { "order", "receipt" } },
    { "instacart%.com",    { "receipt", "order" } },
    { "doordash%.com",     { "receipt", "order" } },
    { "uber%.com",         { "receipt", "trip" } },
    { "lyft%.com",         { "receipt", "ride" } },
    { "grubhub%.com",      { "receipt", "order" } },
    { "airbnb%.com",       { "confirmation", "receipt", "booking" } },
    { "booking%.com",      { "confirmation", "receipt", "booking" } },
    { "expedia%.com",      { "confirmation", "itinerary", "receipt" } },
    { "hotels%.com",       { "confirmation", "receipt" } },
    { "delta%.com",        { "confirmation", "itinerary", "receipt", "e-ticket" } },
    { "united%.com",       { "confirmation", "itinerary", "receipt", "e-ticket" } },
    { "southwest%.com",    { "confirmation", "itinerary", "receipt" } },
    { "americanair%.com",  { "confirmation", "itinerary", "receipt" } },
    { "github%.com",       { "receipt", "invoice", "billing" } },
    { "docker%.com",       { "receipt", "invoice", "billing", "payment" } },
    { "digitalocean%.com", { "receipt", "invoice", "billing" } },
    { "aws%.amazon%.com",  { "invoice", "billing", "receipt" } },
    { "google%.com",       { "receipt", "invoice", "billing", "order" } },
    { "microsoft%.com",    { "receipt", "invoice", "billing", "order" } },
    { "netflix%.com",      { "receipt", "invoice", "billing" } },
    { "spotify%.com",      { "receipt", "invoice", "billing" } },
    { "hulu%.com",         { "receipt", "invoice", "billing" } },
    { "openai%.com",       { "receipt", "invoice", "billing" } },
    { "anthropic%.com",    { "receipt", "invoice", "billing" } },
    { "contabo%.com",      { "receipt", "invoice", "billing", "payment", "order" } },
    { "starbucks%.com",    { "receipt", "invoice", "payment", "reload", "order", "auto reload" } },
}

for _, pair in ipairs(domain_pairs) do
    if sender:match(pair[1]) and contains_any(subject, pair[2]) then
        return move("@Receipts", "Receipt from " .. pair[1]:gsub("%%", ""))
    end
end

return skip()
