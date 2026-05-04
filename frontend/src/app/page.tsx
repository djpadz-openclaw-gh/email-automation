'use client';

import { useState, useEffect } from 'react';
import api, { Rule, AuthResponse } from '@/lib/api';
import RuleEditor from '@/components/RuleEditor';
import RuleList from '@/components/RuleList';
import AccountList from '@/components/AccountList';
import LogViewer from '@/components/LogViewer';
import LoginForm from '@/components/LoginForm';
import RegisterForm from '@/components/RegisterForm';
import Settings from '@/components/Settings';
import DeferredActions from '@/components/DeferredActions';
import AdminUsers from '@/components/AdminUsers';

type Tab = 'rules' | 'accounts' | 'logs' | 'pending' | 'admin' | 'settings';
type AuthView = 'login' | 'register';

export default function Home() {
  const [token, setToken] = useState<string>('');
  const [username, setUsername] = useState<string>('');
  const [authView, setAuthView] = useState<AuthView>('login');
  const [activeTab, setActiveTab] = useState<Tab>('rules');
  const [editingRule, setEditingRule] = useState<Rule | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [registrationEnabled, setRegistrationEnabled] = useState(true);

  useEffect(() => {
    const stored = localStorage.getItem('ea_token');
    const storedUser = localStorage.getItem('ea_username');
    if (stored) {
      setToken(stored);
      api.setToken(stored);
      if (storedUser) setUsername(storedUser);
    }

    // Handle 401 responses
    api.setOnUnauthorized(() => {
      handleLogout();
    });

    // Fetch config to check if registration is enabled
    api.getConfig().then(config => {
      setRegistrationEnabled(config.registration_enabled);
    }).catch(err => {
      console.error('Failed to fetch config:', err);
      // Default to true if fetch fails
      setRegistrationEnabled(true);
    });
  }, []);

  const handleLogin = (response: AuthResponse) => {
    setToken(response.token);
    setUsername(response.user.username);
    api.setToken(response.token);
    localStorage.setItem('ea_token', response.token);
    localStorage.setItem('ea_username', response.user.username);
  };

  const handleLogout = async () => {
    try {
      if (token) await api.logout();
    } catch { /* ignore */ }
    setToken('');
    setUsername('');
    api.setToken('');
    localStorage.removeItem('ea_token');
    localStorage.removeItem('ea_username');
  };

  if (!token) {
    if (authView === 'register' && registrationEnabled) {
      return (
        <RegisterForm
          onComplete={handleLogin}
          onSwitchToLogin={() => setAuthView('login')}
        />
      );
    }
    return (
      <LoginForm
        onLogin={handleLogin}
        onSwitchToRegister={registrationEnabled ? () => setAuthView('register') : undefined}
      />
    );
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
            <div className="flex items-center gap-4">
              <span className="text-sm text-gray-500 dark:text-gray-400">
                {username}
              </span>
              <button
                onClick={() => setActiveTab('settings')}
                className="text-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                title="Settings"
              >
                ⚙️
              </button>
              <button
                onClick={handleLogout}
                className="text-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
              >
                Sign Out
              </button>
            </div>
          </div>
        </div>
      </header>

      {/* Tabs */}
      {activeTab !== 'settings' && (
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 mt-6">
          <nav className="flex gap-1 bg-white dark:bg-gray-900 rounded-lg p-1 shadow-sm border border-gray-200 dark:border-gray-800 w-fit" aria-label="Main navigation">
            {([
              { id: 'rules' as Tab, label: 'Rules', icon: '⚡' },
              { id: 'accounts' as Tab, label: 'Accounts', icon: '📬' },
              { id: 'logs' as Tab, label: 'Activity Log', icon: '📋' },
              { id: 'pending' as Tab, label: 'Pending', icon: '⏳' },
              { id: 'admin' as Tab, label: 'Admin', icon: '🔧' },
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
      )}

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
        {activeTab === 'pending' && <DeferredActions />}
        {activeTab === 'admin' && <AdminUsers />}
        {activeTab === 'settings' && <Settings onBack={() => setActiveTab('rules')} />}
      </main>
    </div>
  );
}
