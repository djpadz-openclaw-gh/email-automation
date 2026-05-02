'use client';

import { useState, useEffect } from 'react';
import api, { Rule } from '@/lib/api';
import RuleEditor from '@/components/RuleEditor';
import RuleList from '@/components/RuleList';
import AccountList from '@/components/AccountList';
import LogViewer from '@/components/LogViewer';
import ApiKeyPrompt from '@/components/ApiKeyPrompt';

type Tab = 'rules' | 'accounts' | 'logs';

export default function Home() {
  const [apiKey, setApiKey] = useState<string>('');
  const [activeTab, setActiveTab] = useState<Tab>('rules');
  const [editingRule, setEditingRule] = useState<Rule | null>(null);
  const [isCreating, setIsCreating] = useState(false);

  useEffect(() => {
    const stored = localStorage.getItem('ea_api_key');
    if (stored) {
      setApiKey(stored);
      api.setApiKey(stored);
    }
  }, []);

  const handleSetApiKey = (key: string) => {
    setApiKey(key);
    api.setApiKey(key);
    localStorage.setItem('ea_api_key', key);
  };

  const handleLogout = () => {
    setApiKey('');
    localStorage.removeItem('ea_api_key');
  };

  if (!apiKey) {
    return <ApiKeyPrompt onSubmit={handleSetApiKey} />;
  }

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-950">
      {/* Header */}
      <header className="bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between items-center h-16">
            <div className="flex items-center gap-3">
              <span className="text-2xl" role="img" aria-label="email">📧</span>
              <h1 className="text-xl font-semibold text-gray-900 dark:text-white">
                Email Automation
              </h1>
            </div>
            <button
              onClick={handleLogout}
              className="text-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
            >
              Disconnect
            </button>
          </div>
        </div>
      </header>

      {/* Tabs */}
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 mt-6">
        <nav className="flex gap-1 bg-white dark:bg-gray-900 rounded-lg p-1 shadow-sm border border-gray-200 dark:border-gray-800 w-fit" aria-label="Main navigation">
          {([
            { id: 'rules' as Tab, label: 'Rules', icon: '⚡' },
            { id: 'accounts' as Tab, label: 'Accounts', icon: '📬' },
            { id: 'logs' as Tab, label: 'Activity Log', icon: '📋' },
          ]).map((tab) => (
            <button
              key={tab.id}
              onClick={() => {
                setActiveTab(tab.id);
                setEditingRule(null);
                setIsCreating(false);
              }}
              className={`px-4 py-2 rounded-md text-sm font-medium transition-colors ${
                activeTab === tab.id
                  ? 'bg-blue-600 text-white'
                  : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800'
              }`}
            >
              <span className="mr-1.5">{tab.icon}</span>
              {tab.label}
            </button>
          ))}
        </nav>
      </div>

      {/* Content */}
      <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
        {activeTab === 'rules' && !editingRule && !isCreating && (
          <RuleList
            onEdit={setEditingRule}
            onCreate={() => setIsCreating(true)}
          />
        )}

        {activeTab === 'rules' && (editingRule || isCreating) && (
          <RuleEditor
            rule={editingRule}
            onSave={() => {
              setEditingRule(null);
              setIsCreating(false);
            }}
            onCancel={() => {
              setEditingRule(null);
              setIsCreating(false);
            }}
          />
        )}

        {activeTab === 'accounts' && <AccountList />}
        {activeTab === 'logs' && <LogViewer />}
      </main>
    </div>
  );
}
