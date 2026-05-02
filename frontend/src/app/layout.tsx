import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'Email Automation',
  description: 'Lua-powered email rule engine',
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-gray-50 dark:bg-gray-950">
        {children}
      </body>
    </html>
  );
}
