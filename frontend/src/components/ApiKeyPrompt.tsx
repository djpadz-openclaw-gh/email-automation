'use client';

import { useState } from 'react';

interface ApiKeyPromptProps {
  onSubmit: (key: string) => void;
}

export default function ApiKeyPrompt({ onSubmit }: ApiKeyPromptProps) {
  const [key, setKey] = useState('');

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-950">
      <div className="bg-white dark:bg-gray-900 rounded-xl shadow-lg p-8 max-w-md w-full border border-gray-200 dark:border-gray-800">
        <div className="text-center mb-6">
          <span className="text-4xl mb-4 block" role="img" aria-label="email">📧</span>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Email Automation</h1>
          <p className="text-gray-500 dark:text-gray-400 mt-2">Enter your API key to connect</p>
        </div>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (key.trim()) onSubmit(key.trim());
          }}
        >
          <label htmlFor="api-key" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
            API Key
          </label>
          <input
            id="api-key"
            type="password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="ea_..."
            className="w-full px-4 py-3 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
            autoFocus
          />
          <button
            type="submit"
            disabled={!key.trim()}
            className="w-full mt-4 px-4 py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            Connect
          </button>
        </form>
      </div>
    </div>
  );
}
