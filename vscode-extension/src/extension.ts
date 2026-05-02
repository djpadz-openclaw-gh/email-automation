import * as vscode from 'vscode';
import { ApiClient } from './apiClient';
import { LuaCompletionProvider } from './completionProvider';
import { RuleDiagnostics } from './diagnostics';
import { RuleTestPanel } from './testPanel';
import { RuleSync } from './ruleSync';

let diagnostics: RuleDiagnostics;
let apiClient: ApiClient;

export function activate(context: vscode.ExtensionContext) {
    const config = vscode.workspace.getConfiguration('emailAutomation');
    apiClient = new ApiClient(
        config.get<string>('apiUrl', 'http://localhost:8080'),
        config.get<string>('apiKey', '')
    );

    diagnostics = new RuleDiagnostics(apiClient);
    const completionProvider = new LuaCompletionProvider();
    const ruleSync = new RuleSync(apiClient);
    const testPanel = new RuleTestPanel(apiClient, context.extensionUri);

    // Completion provider for Lua files
    context.subscriptions.push(
        vscode.languages.registerCompletionItemProvider(
            { language: 'lua' },
            completionProvider,
            '.', ':', '('
        )
    );

    // Hover provider for email context and helpers
    context.subscriptions.push(
        vscode.languages.registerHoverProvider(
            { language: 'lua' },
            completionProvider
        )
    );

    // Diagnostics on save
    if (config.get<boolean>('validateOnSave', true)) {
        context.subscriptions.push(
            vscode.workspace.onDidSaveTextDocument((doc) => {
                if (doc.languageId === 'lua') {
                    diagnostics.validate(doc);
                }
            })
        );
    }

    // Diagnostics on type (debounced)
    if (config.get<boolean>('validateOnType', false)) {
        let timeout: NodeJS.Timeout | undefined;
        context.subscriptions.push(
            vscode.workspace.onDidChangeTextDocument((e) => {
                if (e.document.languageId === 'lua') {
                    if (timeout) { clearTimeout(timeout); }
                    timeout = setTimeout(() => diagnostics.validate(e.document), 1000);
                }
            })
        );
    }

    // Commands
    context.subscriptions.push(
        vscode.commands.registerCommand('emailAutomation.validate', () => {
            const editor = vscode.window.activeTextEditor;
            if (editor && editor.document.languageId === 'lua') {
                diagnostics.validate(editor.document);
            }
        }),

        vscode.commands.registerCommand('emailAutomation.test', () => {
            const editor = vscode.window.activeTextEditor;
            if (editor && editor.document.languageId === 'lua') {
                testPanel.testWithDefaults(editor.document);
            }
        }),

        vscode.commands.registerCommand('emailAutomation.testWithContext', () => {
            const editor = vscode.window.activeTextEditor;
            if (editor && editor.document.languageId === 'lua') {
                testPanel.testWithCustomContext(editor.document);
            }
        }),

        vscode.commands.registerCommand('emailAutomation.pushRule', () => {
            const editor = vscode.window.activeTextEditor;
            if (editor && editor.document.languageId === 'lua') {
                ruleSync.pushRule(editor.document);
            }
        }),

        vscode.commands.registerCommand('emailAutomation.pullRules', () => {
            ruleSync.pullRules();
        }),

        vscode.commands.registerCommand('emailAutomation.listRules', () => {
            ruleSync.listRules();
        }),

        vscode.commands.registerCommand('emailAutomation.setApiKey', async () => {
            const key = await vscode.window.showInputBox({
                prompt: 'Enter your Email Automation API key',
                password: true,
                placeHolder: 'ea_...',
            });
            if (key) {
                await config.update('apiKey', key, vscode.ConfigurationTarget.Global);
                apiClient.setApiKey(key);
                vscode.window.showInformationMessage('API key updated.');
            }
        })
    );

    // React to config changes
    context.subscriptions.push(
        vscode.workspace.onDidChangeConfiguration((e) => {
            if (e.affectsConfiguration('emailAutomation')) {
                const cfg = vscode.workspace.getConfiguration('emailAutomation');
                apiClient.setUrl(cfg.get<string>('apiUrl', 'http://localhost:8080'));
                apiClient.setApiKey(cfg.get<string>('apiKey', ''));
            }
        })
    );

    // Status bar item
    const statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
    statusBar.text = '$(mail) Email Rules';
    statusBar.command = 'emailAutomation.validate';
    statusBar.tooltip = 'Validate email rule (Ctrl+Shift+V)';
    context.subscriptions.push(statusBar);

    context.subscriptions.push(
        vscode.window.onDidChangeActiveTextEditor((editor) => {
            if (editor && editor.document.languageId === 'lua') {
                statusBar.show();
            } else {
                statusBar.hide();
            }
        })
    );

    // Show on activation if current editor is Lua
    if (vscode.window.activeTextEditor?.document.languageId === 'lua') {
        statusBar.show();
    }

    console.log('Email Automation extension activated');
}

export function deactivate() {
    diagnostics?.dispose();
}
