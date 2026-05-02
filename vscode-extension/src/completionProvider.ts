import * as vscode from 'vscode';

// Email context fields available in Lua rules
const EMAIL_FIELDS: Array<{ name: string; type: string; doc: string }> = [
    { name: 'message_id', type: 'string', doc: 'Unique message identifier' },
    { name: 'subject', type: 'string', doc: 'Email subject line' },
    { name: 'sender_name', type: 'string', doc: 'Display name of the sender' },
    { name: 'sender_address', type: 'string', doc: 'Email address of the sender' },
    { name: 'recipients', type: 'table', doc: 'List of recipient email addresses' },
    { name: 'date', type: 'string', doc: 'Email date (ISO 8601)' },
    { name: 'age_seconds', type: 'number', doc: 'Age of the email in seconds' },
    { name: 'body_preview', type: 'string', doc: 'First ~200 chars of the email body' },
    { name: 'has_attachments', type: 'boolean', doc: 'Whether the email has attachments' },
    { name: 'attachment_names', type: 'table', doc: 'List of attachment filenames' },
    { name: 'attachment_types', type: 'table', doc: 'List of attachment MIME types' },
    { name: 'headers', type: 'table', doc: 'Email headers as key-value pairs (e.g. headers["Authentication-Results"])' },
    { name: 'folder', type: 'string', doc: 'Current IMAP folder name' },
    { name: 'account_id', type: 'number', doc: 'ID of the email account' },
];

// Helper functions available in the Lua sandbox
const HELPER_FUNCTIONS: Array<{ name: string; signature: string; doc: string }> = [
    { name: 'contains', signature: 'contains(haystack, needle)', doc: 'Case-insensitive substring check. Returns boolean.' },
    { name: 'contains_any', signature: 'contains_any(haystack, {needle1, needle2, ...})', doc: 'Case-insensitive check if haystack contains any of the needles. Returns boolean.' },
    { name: 'matches', signature: 'matches(text, pattern)', doc: 'Case-insensitive pattern match. Returns boolean.' },
    { name: 'ends_with', signature: 'ends_with(text, suffix)', doc: 'Case-insensitive suffix check. Returns boolean.' },
    { name: 'starts_with', signature: 'starts_with(text, prefix)', doc: 'Case-insensitive prefix check. Returns boolean.' },
    { name: 'domain_of', signature: 'domain_of(email_address)', doc: 'Extract domain from an email address. Returns string.' },
    { name: 'older_than', signature: 'older_than(seconds)', doc: 'Check if email is older than N seconds. Returns boolean.' },
    { name: 'older_than_hours', signature: 'older_than_hours(hours)', doc: 'Check if email is older than N hours. Returns boolean.' },
    { name: 'older_than_days', signature: 'older_than_days(days)', doc: 'Check if email is older than N days. Returns boolean.' },
    { name: 'has_attachment_type', signature: 'has_attachment_type(mime_type)', doc: 'Check if email has an attachment of the given MIME type. Returns boolean.' },
    { name: 'has_ics', signature: 'has_ics()', doc: 'Check if email has a calendar (.ics) attachment. Returns boolean.' },
    { name: 'now_hour', signature: 'now_hour()', doc: 'Current hour of day (0-23). Returns number.' },
    { name: 'is_reply', signature: 'is_reply()', doc: 'Check if email is a reply (has In-Reply-To header or Re: subject). Returns boolean.' },
];

// Action functions that set the rule result
const ACTION_FUNCTIONS: Array<{ name: string; signature: string; doc: string }> = [
    { name: 'skip', signature: 'skip()', doc: 'Skip this email — no action taken, continue to next rule.' },
    { name: 'delete', signature: 'delete(reason)', doc: 'Delete the email.' },
    { name: 'archive', signature: 'archive(reason)', doc: 'Archive the email (move to All Mail/Archive).' },
    { name: 'move', signature: 'move(folder, reason)', doc: 'Move the email to the specified IMAP folder.' },
    { name: 'keep', signature: 'keep(reason)', doc: 'Keep the email in inbox and stop processing further rules.' },
    { name: 'notify', signature: 'notify(message, reason)', doc: 'Send a Telegram notification about this email.' },
    { name: 'defer_action', signature: 'defer_action(action, delay_hours, target, reason)', doc: 'Schedule an action to run after a delay.' },
    { name: 'move_after', signature: 'move_after(hours, folder, reason)', doc: 'Move the email to a folder after N hours.' },
    { name: 'delete_after', signature: 'delete_after(hours, reason)', doc: 'Delete the email after N hours.' },
];

export class LuaCompletionProvider implements vscode.CompletionItemProvider, vscode.HoverProvider {

    provideCompletionItems(
        document: vscode.TextDocument,
        position: vscode.Position,
    ): vscode.CompletionItem[] {
        const lineText = document.lineAt(position).text;
        const linePrefix = lineText.substring(0, position.character);

        const items: vscode.CompletionItem[] = [];

        // After "email." — suggest email context fields
        if (linePrefix.endsWith('email.')) {
            for (const field of EMAIL_FIELDS) {
                const item = new vscode.CompletionItem(field.name, vscode.CompletionItemKind.Field);
                item.detail = `(${field.type}) email.${field.name}`;
                item.documentation = new vscode.MarkdownString(field.doc);
                item.sortText = `0_${field.name}`;
                items.push(item);
            }
            return items;
        }

        // After "email:" — suggest string methods on email fields
        if (/email\.\w+:$/.test(linePrefix)) {
            const stringMethods = [
                { name: 'lower', sig: ':lower()', doc: 'Convert to lowercase' },
                { name: 'upper', sig: ':upper()', doc: 'Convert to uppercase' },
                { name: 'find', sig: ':find(pattern, init, plain)', doc: 'Find pattern in string' },
                { name: 'sub', sig: ':sub(i, j)', doc: 'Extract substring' },
                { name: 'len', sig: ':len()', doc: 'String length' },
                { name: 'match', sig: ':match(pattern)', doc: 'Pattern match' },
                { name: 'gsub', sig: ':gsub(pattern, repl)', doc: 'Global substitution' },
                { name: 'rep', sig: ':rep(n)', doc: 'Repeat string n times' },
            ];
            for (const m of stringMethods) {
                const item = new vscode.CompletionItem(m.name, vscode.CompletionItemKind.Method);
                item.detail = m.sig;
                item.documentation = m.doc;
                items.push(item);
            }
            return items;
        }

        // General completions — helper and action functions
        for (const fn of HELPER_FUNCTIONS) {
            const item = new vscode.CompletionItem(fn.name, vscode.CompletionItemKind.Function);
            item.detail = fn.signature;
            item.documentation = new vscode.MarkdownString(fn.doc);
            item.sortText = `1_${fn.name}`;
            items.push(item);
        }

        for (const fn of ACTION_FUNCTIONS) {
            const item = new vscode.CompletionItem(fn.name, vscode.CompletionItemKind.Function);
            item.detail = fn.signature;
            item.documentation = new vscode.MarkdownString(`**Action:** ${fn.doc}`);
            item.sortText = `2_${fn.name}`;
            items.push(item);
        }

        // email global
        const emailItem = new vscode.CompletionItem('email', vscode.CompletionItemKind.Variable);
        emailItem.detail = '(table) Email context';
        emailItem.documentation = new vscode.MarkdownString('The email being evaluated. Access fields with `email.subject`, `email.sender_address`, etc.');
        emailItem.sortText = '0_email';
        items.push(emailItem);

        return items;
    }

    provideHover(
        document: vscode.TextDocument,
        position: vscode.Position,
    ): vscode.Hover | undefined {
        const range = document.getWordRangeAtPosition(position, /[\w.]+/);
        if (!range) { return undefined; }
        const word = document.getText(range);

        // email.field hover
        if (word.startsWith('email.')) {
            const fieldName = word.substring(6);
            const field = EMAIL_FIELDS.find(f => f.name === fieldName);
            if (field) {
                return new vscode.Hover(
                    new vscode.MarkdownString(`**email.${field.name}** \`${field.type}\`\n\n${field.doc}`)
                );
            }
        }

        // Helper function hover
        const helper = HELPER_FUNCTIONS.find(f => f.name === word);
        if (helper) {
            return new vscode.Hover(
                new vscode.MarkdownString(`**${helper.signature}**\n\n${helper.doc}`)
            );
        }

        // Action function hover
        const action = ACTION_FUNCTIONS.find(f => f.name === word);
        if (action) {
            return new vscode.Hover(
                new vscode.MarkdownString(`**${action.signature}**\n\n🎯 *Action:* ${action.doc}`)
            );
        }

        return undefined;
    }
}
