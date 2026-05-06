'use client';

import { useState, useEffect } from 'react';
import api, { Tenant } from '@/lib/api';

interface AdminPanelProps {
  onBack: () => void;
}

export default function AdminPanel({ onBack }: AdminPanelProps) {
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [newName, setNewName] = useState('');
  const [newSlug, setNewSlug] = useState('');
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    loadTenants();
  }, []);

  const loadTenants = async () => {
    try {
      setLoading(true);
      setError('');
      const data = await api.adminListTenants();
      setTenants(data || []);
    } catch (err: unknown) {
      if (err instanceof Error && err.message.includes('403')) {
        setError('Access denied. Admin privileges required.');
      } else {
        setError(err instanceof Error ? err.message : 'Failed to load tenants');
      }
    } finally {
      setLoading(false);
    }
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newName.trim() || !newSlug.trim()) {
      setError('Name and slug are required');
      return;
    }
    try {
      setCreating(true);
      setError('');
      setSuccess('');
      await api.adminCreateTenant(newName.trim(), newSlug.trim());
      setSuccess('Tenant created successfully');
      setNewName('');
      setNewSlug('');
      loadTenants();
    } catch (err: unknown) {
      if (err instanceof Error && err.message.includes('403')) {
        setError('Access denied. Admin privileges required.');
      } else {
        setError(err instanceof Error ? err.message : 'Failed to create tenant');
      }
    } finally {
      setCreating(false);
    }
  };

  return (
    <div>
      <div className="flex items-center gap-3 mb-6">
        <button onClick={onBack} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Back">
          ← Back
        </button>
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
          Admin Panel
        </h2>
      </div>

      {(error || success) && (
        <div className={`mb-4 p-3 rounded-lg text-sm ${
          error
            ? 'bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400'
            : 'bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 text-green-700 dark:text-green-400'
        }`} role="alert">
          {error || success}
        </div>
      )}

      {/* Create Tenant */}
      <div className="bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 p-6 mb-6">
        <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-4">Create Tenant</h3>
        <form onSubmit={handleCreate} className="flex items-end gap-3">
          <div className="flex-1">
            <label htmlFor="tenant-name" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Name</label>
            <input
              id="tenant-name"
              type="text"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="My Workspace"
              className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none"
            />
          </div>
          <div className="flex-1">
            <label htmlFor="tenant-slug" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Slug</label>
            <input
              id="tenant-slug"
              type="text"
              value={newSlug}
              onChange={(e) => setNewSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '-'))}
              placeholder="my-workspace"
              className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none"
            />
          </div>
          <button
            type="submit"
            disabled={creating || !newName.trim() || !newSlug.trim()}
            className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors"
          >
            {creating ? 'Creating...' : '+ Create'}
          </button>
        </form>
      </div>

      {/* Tenant List */}
      <div className="bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 p-6">
        <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-4">Tenants</h3>

        {loading ? (
          <p className="text-sm text-gray-500 dark:text-gray-400">Loading...</p>
        ) : tenants.length === 0 ? (
          <p className="text-sm text-gray-500 dark:text-gray-400">No tenants found.</p>
        ) : (
          <div className="space-y-2">
            {tenants.map((tenant) => (
              <div key={tenant.id} className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                <div>
                  <p className="text-sm font-medium text-gray-900 dark:text-white">
                    {tenant.name}
                  </p>
                  <p className="text-xs text-gray-500 dark:text-gray-400">
                    Slug: {tenant.slug} • API Key: {tenant.api_key} • Created {new Date(tenant.created_at).toLocaleDateString()}
                  </p>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
