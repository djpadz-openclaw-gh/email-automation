import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import { ApiClient } from './apiClient';

export class RuleSync {
    private apiClient: ApiClient;

    constructor(apiClient: ApiClient) {
        this.apiClient = apiClient;
    }

    async pushRule(document: vscode.TextDocument): Promise<void> {
        const code = document.getText();
        const fileName = path.basename(document.fileName, '.lua');

        // Extract rule name and description from comments
        const nameMatch = code.match(/^--\s*Rule:\s*(.+)$/m);
        const descMatch = code.match(/^--\s*(?!Rule:)(.+)$/m);

        const name = nameMatch ? nameMatch[1].trim() : fileName;
        const description = descMatch ? descMatch[1].trim() : '';

        // Check if rule already exists on server
        const rulesResult = await this.apiClient.listRules();
        if (!rulesResult.ok) {
            vscode.window.showErrorMessage(`Failed to fetch rules: ${rulesResult.error}`);
            return;
        }

        const existingRule = rulesResult.data?.find(
            r => r.name.toLowerCase() === name.toLowerCase()
        );

        if (existingRule) {
            const action = await vscode.window.showInformationMessage(
                `Rule "${name}" already exists on server (ID: ${existingRule.id}). Update it?`,
                'Update',
                'Create New',
                'Cancel'
            );

            if (action === 'Update') {
                const result = await this.apiClient.updateRule(existingRule.id, {
                    name,
                    description,
                    lua_code: code,
                    priority: existingRule.priority,
                    active: existingRule.active,
                });
                if (result.ok) {
                    vscode.window.showInformationMessage(`✓ Updated rule "${name}" on server`);
                } else {
                    vscode.window.showErrorMessage(`Failed to update: ${result.error}`);
                }
                return;
            } else if (action === 'Cancel') {
                return;
            }
            // Fall through to create new
        }

        // Ask for priority
        const priorityStr = await vscode.window.showInputBox({
            prompt: 'Rule priority (lower = runs first)',
            value: '100',
            validateInput: (v) => /^\d+$/.test(v) ? null : 'Must be a number',
        });
        if (!priorityStr) { return; }

        const result = await this.apiClient.createRule({
            name,
            description,
            lua_code: code,
            priority: parseInt(priorityStr, 10),
            active: true,
        });

        if (result.ok) {
            vscode.window.showInformationMessage(`✓ Created rule "${name}" on server (ID: ${result.data?.id})`);
        } else {
            vscode.window.showErrorMessage(`Failed to create rule: ${result.error}`);
        }
    }

    async pullRules(): Promise<void> {
        const result = await this.apiClient.listRules();
        if (!result.ok) {
            vscode.window.showErrorMessage(`Failed to fetch rules: ${result.error}`);
            return;
        }

        const rules = result.data || [];
        if (rules.length === 0) {
            vscode.window.showInformationMessage('No rules found on server.');
            return;
        }

        // Ask where to save
        const folders = vscode.workspace.workspaceFolders;
        let targetDir: string;

        if (folders && folders.length > 0) {
            const rulesDir = path.join(folders[0].uri.fsPath, 'rules');
            const customDir = await vscode.window.showInputBox({
                prompt: 'Directory to save rules',
                value: rulesDir,
            });
            if (!customDir) { return; }
            targetDir = customDir;
        } else {
            const uri = await vscode.window.showOpenDialog({
                canSelectFolders: true,
                canSelectFiles: false,
                openLabel: 'Select rules directory',
            });
            if (!uri || uri.length === 0) { return; }
            targetDir = uri[0].fsPath;
        }

        // Create directory if needed
        if (!fs.existsSync(targetDir)) {
            fs.mkdirSync(targetDir, { recursive: true });
        }

        let pulled = 0;
        for (const rule of rules) {
            const slug = rule.name.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_|_$/g, '');
            const filePath = path.join(targetDir, `${slug}.lua`);

            // Add metadata header
            const header = [
                `-- Rule: ${rule.name}`,
                rule.description ? `-- ${rule.description}` : null,
                `-- Server ID: ${rule.id} | Priority: ${rule.priority} | Active: ${rule.active}`,
                '',
            ].filter(Boolean).join('\n');

            const content = rule.lua_code.startsWith('-- Rule:')
                ? rule.lua_code
                : header + rule.lua_code;

            fs.writeFileSync(filePath, content, 'utf-8');
            pulled++;
        }

        vscode.window.showInformationMessage(`✓ Pulled ${pulled} rules to ${targetDir}`);

        // Open the directory
        const uri = vscode.Uri.file(targetDir);
        await vscode.commands.executeCommand('revealInExplorer', uri);
    }

    async listRules(): Promise<void> {
        const result = await this.apiClient.listRules();
        if (!result.ok) {
            vscode.window.showErrorMessage(`Failed to fetch rules: ${result.error}`);
            return;
        }

        const rules = result.data || [];
        if (rules.length === 0) {
            vscode.window.showInformationMessage('No rules found on server.');
            return;
        }

        // Show as quick pick
        const items = rules.map(r => ({
            label: `${r.active ? '$(check)' : '$(circle-slash)'} ${r.name}`,
            description: `Priority: ${r.priority} | ID: ${r.id}`,
            detail: r.description || undefined,
            rule: r,
        }));

        const selected = await vscode.window.showQuickPick(items, {
            placeHolder: 'Select a rule to open',
            matchOnDescription: true,
            matchOnDetail: true,
        });

        if (selected) {
            // Open the rule in a new editor
            const doc = await vscode.workspace.openTextDocument({
                content: selected.rule.lua_code,
                language: 'lua',
            });
            await vscode.window.showTextDocument(doc);
        }
    }
}
