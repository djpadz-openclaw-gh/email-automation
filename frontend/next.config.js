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
      // beforeFiles: rewrites checked before filesystem routes (pages/app)
      beforeFiles: [],
      // afterFiles: rewrites checked after filesystem routes but before fallback
      afterFiles: [],
      // fallback: only checked when no filesystem route matches
      // This ensures App Router routes (like /api/oauth2/callback) take priority
      fallback: [
        {
          source: '/auth/:path*',
          destination: `${apiUrl}/auth/:path*`,
        },
        {
          source: '/api/:path*',
          destination: `${apiUrl}/api/:path*`,
        },
        {
          source: '/admin/:path*',
          destination: `${apiUrl}/admin/:path*`,
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
    };
  },
};

module.exports = nextConfig;
