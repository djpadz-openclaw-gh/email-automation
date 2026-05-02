'use client';

import { useState } from 'react';
import { REFERENCE_DOCS, EXAMPLE_RULES } from '@/lib/reference';

type Section = 'context' | 'actions' | 'helpers' | 'stdlib' | 'examples';

export default function ReferencePanel() {
  const [activeSection, setActiveSection] = useState<Section>('context');

  const sections: { id: Section; label: string }[] = [
    { id: 'context', label: 'Email' },
    { id: 'actions', label: 'Actions' },
    { id: 'helpers', label: 'Helpers' },
    { id: 'stdlib', label: 'Stdlib' },
    { id: 'examples', label: 'Examples' },
  ];

  return (
    <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-xl overflow-hidden sticky top-6">
      <div className="bg-purple-50 dark:bg-purple-900/20 px-4 py-3 border-b border-gray-200 dark:border-gray-800">
        <h3 className="text-sm font-semibold text-purple-800 dark:text-purple-300">📖 Reference</h3>
      </div>

      {/* Section tabs */}
      <div className="flex border-b border-gray-200 dark:border-gray-800 overflow-x-auto">
        {sections.map((s) => (
          <button
            key={s.id}
            onClick={() => setActiveSection(s.id)}
            className={`px-3 py-2 text-xs font-medium whitespace-nowrap transition-colors ${
              activeSection === s.id
                ? 'text-purple-700 dark:text-purple-400 border-b-2 border-purple-600'
                : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
            }`}
          >
            {s.label}
          </button>
        ))}
      </div>

      <div className="p-4 max-h-[600px] overflow-y-auto">
        {activeSection === 'context' && (
          <div>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
              {REFERENCE_DOCS.emailContext.description}
            </p>
            <div className="space-y-2">
              {REFERENCE_DOCS.emailContext.fields.map((f) => (
                <div key={f.name} className="text-xs">
                  <code className="text-purple-600 dark:text-purple-400 font-mono">{f.name}</code>
                  <span className="text-gray-400 ml-1">({f.type})</span>
                  <p className="text-gray-500 dark:text-gray-400 ml-2">{f.description}</p>
                </div>
              ))}
            </div>
          </div>
        )}

        {activeSection === 'actions' && (
          <div>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
              {REFERENCE_DOCS.actions.description}
            </p>
            <div className="space-y-2">
              {REFERENCE_DOCS.actions.functions.map((f) => (
                <div key={f.name} className="text-xs">
                  <code className="text-blue-600 dark:text-blue-400 font-mono">{f.name}</code>
                  <p className="text-gray-500 dark:text-gray-400 ml-2">{f.description}</p>
                </div>
              ))}
            </div>
          </div>
        )}

        {activeSection === 'helpers' && (
          <div>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
              {REFERENCE_DOCS.helpers.description}
            </p>
            <div className="space-y-2">
              {REFERENCE_DOCS.helpers.functions.map((f) => (
                <div key={f.name} className="text-xs">
                  <code className="text-green-600 dark:text-green-400 font-mono">{f.name}</code>
                  <p className="text-gray-500 dark:text-gray-400 ml-2">{f.description}</p>
                </div>
              ))}
            </div>
          </div>
        )}

        {activeSection === 'stdlib' && (
          <div>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
              {REFERENCE_DOCS.luaStdlib.description}
            </p>
            <div className="space-y-3">
              {REFERENCE_DOCS.luaStdlib.modules.map((m) => (
                <div key={m.name} className="text-xs">
                  <div className="font-medium text-gray-700 dark:text-gray-300">{m.name}</div>
                  <p className="text-gray-500 dark:text-gray-400 mt-0.5 font-mono text-[10px] leading-relaxed">
                    {m.items}
                  </p>
                </div>
              ))}
            </div>
          </div>
        )}

        {activeSection === 'examples' && (
          <div className="space-y-4">
            {EXAMPLE_RULES.map((ex, i) => (
              <div key={i} className="text-xs">
                <div className="font-medium text-gray-700 dark:text-gray-300">{ex.name}</div>
                <p className="text-gray-500 dark:text-gray-400 mb-1">{ex.description}</p>
                <pre className="bg-gray-50 dark:bg-gray-800 rounded p-2 overflow-x-auto text-[10px] leading-relaxed">
                  <code>{ex.code}</code>
                </pre>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
