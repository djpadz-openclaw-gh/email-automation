import * as vscode from 'vscode';
import { ApiClient } from './apiClient';

export class RuleDiagnostics {
    private collection: vscode.DiagnosticCollection;
    private apiClient: ApiClient;

    constructor(apiClient: ApiClient) {
        this.apiClient = apiClient;
        this.collection = vscode.languages.createDiagnosticCollection('emailAutomation');
    }

    async validate(document: vscode.TextDocument): Promise<void> {
        const code = document.getText();
        if (!code.trim()) {
            this.collection.set(document.uri, []);
            return;
        }

        const result = await this.apiClient.validate(code);

        if (!result.ok) {
            // API connection error — show as warning
            vscode.window.showWarningMessage(`Email Automation: ${result.error}`);
            return;
        }

        const diagnostics: vscode.Diagnostic[] = [];

        if (result.data && !result.data.valid && result.data.error) {
            const errorMsg = result.data.error;

            // Try to parse line number from Lua error format: "<string>:LINE: message"
            let line = 0;
            let column = 0;
            const lineMatch = errorMsg.match(/<string>:(\d+):\s*(.*)/);
            if (lineMatch) {
                line = Math.max(0, parseInt(lineMatch[1], 10) - 1);
            }
            if (result.data.line) {
                line = Math.max(0, result.data.line - 1);
            }
            if (result.data.column) {
                column = Math.max(0, result.data.column - 1);
            }

            // Highlight the entire line
            const lineLength = document.lineAt(Math.min(line, document.lineCount - 1)).text.length;
            const range = new vscode.Range(line, column, line, lineLength);

            const diagnostic = new vscode.Diagnostic(
                range,
                errorMsg,
                vscode.DiagnosticSeverity.Error
            );
            diagnostic.source = 'Email Automation';
            diagnostics.push(diagnostic);
        }

        this.collection.set(document.uri, diagnostics);

        if (diagnostics.length === 0 && result.data?.valid) {
            vscode.window.showInformationMessage('✓ Lua rule is valid');
        }
    }

    dispose(): void {
        this.collection.dispose();
    }
}
