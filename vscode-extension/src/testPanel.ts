import * as vscode from 'vscode';
import { ApiClient } from './apiClient';

const DEFAULT_EMAIL_CONTEXT = {
    message_id: '<test-001@example.com>',
    subject: 'Your order has shipped - Order #123-4567890',
    sender_name: 'Amazon.com',
    sender_address: 'shipment-tracking@amazon.com',
    recipients: ['user@example.com'],
    date: new Date().toISOString(),
    age_seconds: 90000, // ~25 hours
    body_preview: 'Your package is on its way! Track your shipment at...',
    has_attachments: false,
    attachment_names: [],
    attachment_types: [],
    headers: {
        'Authentication-Results': 'spf=pass; dkim=pass; dmarc=pass',
        'List-Unsubscribe': '<mailto:unsubscribe@amazon.com>',
    },
    folder: 'INBOX',
    account_id: 1,
};

export class RuleTestPanel {
    private apiClient: ApiClient;
    private extensionUri: vscode.Uri;
    private outputChannel: vscode.OutputChannel;

    constructor(apiClient: ApiClient, extensionUri: vscode.Uri) {
        this.apiClient = apiClient;
        this.extensionUri = extensionUri;
        this.outputChannel = vscode.window.createOutputChannel('Email Rule Test');
    }

    async testWithDefaults(document: vscode.TextDocument): Promise<void> {
        const code = document.getText();
        await this.runTest(code, DEFAULT_EMAIL_CONTEXT);
    }

    async testWithCustomContext(document: vscode.TextDocument): Promise<void> {
        const code = document.getText();

        // Open an input document with the default context for editing
        const contextDoc = await vscode.workspace.openTextDocument({
            content: JSON.stringify(DEFAULT_EMAIL_CONTEXT, null, 2),
            language: 'json',
        });

        const editor = await vscode.window.showTextDocument(contextDoc, {
            viewColumn: vscode.ViewColumn.Beside,
            preview: true,
        });

        // Show a message with instructions
        const action = await vscode.window.showInformationMessage(
            'Edit the email context JSON, then click "Run Test"',
            'Run Test',
            'Cancel'
        );

        if (action === 'Run Test') {
            try {
                const contextText = editor.document.getText();
                const emailContext = JSON.parse(contextText);
                await this.runTest(code, emailContext);
            } catch (e) {
                vscode.window.showErrorMessage(`Invalid JSON: ${e}`);
            }
        }
    }

    private async runTest(code: string, emailContext: Record<string, unknown>): Promise<void> {
        this.outputChannel.clear();
        this.outputChannel.show(true);
        this.outputChannel.appendLine('━━━ Email Rule Test ━━━');
        this.outputChannel.appendLine('');

        // Show what we're testing against
        this.outputChannel.appendLine(`Subject: ${emailContext.subject}`);
        this.outputChannel.appendLine(`From: ${emailContext.sender_name} <${emailContext.sender_address}>`);
        this.outputChannel.appendLine(`Age: ${Math.round((emailContext.age_seconds as number) / 3600)}h`);
        this.outputChannel.appendLine(`Folder: ${emailContext.folder}`);
        this.outputChannel.appendLine('');
        this.outputChannel.appendLine('Running rule...');

        const result = await this.apiClient.testRule(code, emailContext);

        if (!result.ok) {
            this.outputChannel.appendLine(`❌ Error: ${result.error}`);
            vscode.window.showErrorMessage(`Test failed: ${result.error}`);
            return;
        }

        const data = result.data!;
        this.outputChannel.appendLine('');

        if (data.error) {
            this.outputChannel.appendLine(`❌ Lua Error: ${data.error}`);
            vscode.window.showErrorMessage(`Lua error: ${data.error}`);
            return;
        }

        const actionIcons: Record<string, string> = {
            skip: '⏭️',
            delete: '🗑️',
            archive: '📦',
            move: '📁',
            keep: '📌',
            notify: '🔔',
            defer: '⏰',
        };

        const icon = actionIcons[data.action] || '❓';

        this.outputChannel.appendLine(`${icon} Action: ${data.action.toUpperCase()}`);
        if (data.target) {
            this.outputChannel.appendLine(`   Target: ${data.target}`);
        }
        if (data.delay > 0) {
            this.outputChannel.appendLine(`   Delay: ${data.delay}s (${Math.round(data.delay / 3600)}h)`);
        }
        if (data.reason) {
            this.outputChannel.appendLine(`   Reason: ${data.reason}`);
        }

        this.outputChannel.appendLine('');
        this.outputChannel.appendLine('━━━ Test Complete ━━━');

        if (data.action === 'skip') {
            vscode.window.showInformationMessage('⏭️ Rule result: SKIP (no action)');
        } else {
            vscode.window.showInformationMessage(`${icon} Rule result: ${data.action.toUpperCase()}${data.target ? ' → ' + data.target : ''}`);
        }
    }
}
