/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  reactStrictMode: true,
  env: {
    API_URL: process.env.API_URL || 'http://api.email-automation.svc.cluster.local:8080',
  },
  async rewrites() {
    const apiUrl = process.env.API_URL || 'http://api.email-automation.svc.cluster.local:8080';
    return [
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
    ];
  },
};

module.exports = nextConfig;
