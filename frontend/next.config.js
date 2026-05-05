/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  reactStrictMode: true,
  env: {
    API_URL: process.env.API_URL || 'http://api.email-automation.svc.cluster.local:8080',
  },
  async rewrites() {
    const apiUrl = process.env.API_URL || 'http://api.email-automation.svc.cluster.local:8080';
    return {
      // beforeFiles rewrites are checked before pages/public files
      // but AFTER API route handlers in the app directory
      beforeFiles: [],
      // afterFiles rewrites are checked after pages/public files
      // This ensures our /api/oauth2/callback route handler takes precedence
      afterFiles: [
        {
          source: '/auth/:path*',
          destination: `${apiUrl}/auth/:path*`,
        },
        {
          source: '/api/v1/:path*',
          destination: `${apiUrl}/api/v1/:path*`,
        },
        {
          source: '/api/kiro/:path*',
          destination: `${apiUrl}/api/kiro/:path*`,
        },
        {
          source: '/config',
          destination: `${apiUrl}/config`,
        },
        {
          source: '/health',
          destination: `${apiUrl}/health`,
        },
        {
          source: '/ready',
          destination: `${apiUrl}/ready`,
        },
      ],
      // fallback rewrites are checked after both pages and afterFiles
      fallback: [],
    };
  },
};

module.exports = nextConfig;
