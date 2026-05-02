# Email Automation - Lua Rules (VS Code Extension)

Edit, validate, and test Lua email automation rules directly from VS Code.

## Features

- **Autocomplete** for email context fields (`email.subject`, `email.sender_address`, etc.), helper functions (`contains_any`, `older_than_hours`), and action functions (`move`, `delete`, `archive`)
- **Hover documentation** for all email fields, helpers, and actions
- **Inline validation** via the API — errors highlighted in the editor with line numbers
- **Rule testing** — run rules against sample email contexts and see results in the output panel
- **Rule sync** — push local `.lua` files to the server, pull rules down for editing
- **Snippets** — templates for common rule patterns (`rule`, `rule-keywords`, `rule-defer`, etc.)

## Setup

1. Install the extension
2. Set your API URL and key:
   - `Cmd+Shift+P` → "Email Automation: Set API Key"
   - Or edit settings: `emailAutomation.apiUrl` and `emailAutomation.apiKey`

## Commands

| Command | Keybinding | Description |
|---------|-----------|-------------|
| Validate Rule | `Cmd+Shift+V` | Validate Lua syntax via the API |
| Test Rule | `Cmd+Shift+T` | Test rule against a sample email |
| Test with Custom Context | — | Test with editable email JSON |
| Push Rule | — | Upload current file to the server |
| Pull Rules | — | Download all rules from server |
| List Rules | — | Browse server rules in quick pick |
| Set API Key | — | Configure API authentication |

## Snippets

Type these prefixes in a `.lua` file:

| Prefix | Description |
|--------|-------------|
| `rule` | Basic rule template |
| `rule-keywords` | Rule with domain + keyword matching |
| `rule-defer` | Rule with deferred action |
| `skip` | Skip action |
| `move` | Move to folder |
| `del` | Delete email |
| `arch` | Archive email |
| `keep` | Keep in inbox |
| `notif` | Send notification |
| `moveafter` | Move after delay |
| `delafter` | Delete after delay |
| `domcheck` | Domain check pattern |
| `agecheck` | Age check pattern |
| `containsany` | Contains-any check |
| `attach` | Attachment check |
| `ics` | Calendar attachment check |
| `isreply` | Reply check |

## Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `emailAutomation.apiUrl` | `http://localhost:8080` | API server URL |
| `emailAutomation.apiKey` | — | API key for authentication |
| `emailAutomation.validateOnSave` | `true` | Auto-validate on save |
| `emailAutomation.validateOnType` | `false` | Validate as you type (debounced) |

## Development

```bash
cd vscode-extension
npm install
npm run compile
# Press F5 in VS Code to launch Extension Development Host
```

## Packaging

```bash
npm run package
# Produces email-automation-lua-0.1.0.vsix
```
