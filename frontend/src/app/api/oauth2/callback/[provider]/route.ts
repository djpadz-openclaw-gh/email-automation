import { NextRequest, NextResponse } from 'next/server';

/**
 * OAuth2 callback handler - proxies the callback to the backend
 * and returns an HTML page that auto-closes the popup window.
 */
export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ provider: string }> }
) {
  const { provider } = await params;
  const searchParams = request.nextUrl.searchParams;
  const code = searchParams.get('code');
  const state = searchParams.get('state');
  const error = searchParams.get('error');
  const errorDescription = searchParams.get('error_description');

  // If OAuth provider returned an error
  if (error) {
    return new NextResponse(renderHTML(false, errorDescription || error), {
      headers: { 'Content-Type': 'text/html' },
    });
  }

  if (!code || !state) {
    return new NextResponse(renderHTML(false, 'Missing code or state parameter'), {
      headers: { 'Content-Type': 'text/html' },
    });
  }

  // Forward the callback to the backend
  const apiUrl = process.env.API_URL || 'http://api.email-automation.svc.cluster.local:8080';
  const backendUrl = `${apiUrl}/api/oauth2/callback/${provider}?code=${encodeURIComponent(code)}&state=${encodeURIComponent(state)}`;

  try {
    const res = await fetch(backendUrl);
    const body = await res.json();

    if (!res.ok) {
      return new NextResponse(renderHTML(false, body.error || 'Connection failed'), {
        headers: { 'Content-Type': 'text/html' },
      });
    }

    const email = body.account?.email || '';
    return new NextResponse(renderHTML(true, `Account connected: ${email}`), {
      headers: { 'Content-Type': 'text/html' },
    });
  } catch (err) {
    return new NextResponse(renderHTML(false, 'Failed to connect to backend'), {
      headers: { 'Content-Type': 'text/html' },
    });
  }
}

function renderHTML(success: boolean, message: string): string {
  const icon = success ? '✅' : '❌';
  const title = success ? 'Account Connected' : 'Connection Failed';
  const bgColor = success ? '#f0fdf4' : '#fef2f2';
  const textColor = success ? '#166534' : '#991b1b';

  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>${title}</title>
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      margin: 0;
      background: ${bgColor};
    }
    .container {
      text-align: center;
      padding: 2rem;
    }
    .icon { font-size: 3rem; margin-bottom: 1rem; }
    .title { font-size: 1.25rem; font-weight: 600; color: ${textColor}; margin-bottom: 0.5rem; }
    .message { color: #6b7280; font-size: 0.875rem; margin-bottom: 1.5rem; }
    .closing { color: #9ca3af; font-size: 0.75rem; }
  </style>
</head>
<body>
  <div class="container">
    <div class="icon">${icon}</div>
    <div class="title">${title}</div>
    <div class="message">${message}</div>
    <div class="closing">${success ? 'This window will close automatically...' : 'You can close this window.'}</div>
  </div>
  <script>
    ${success ? `
    // Notify opener and close after a short delay
    if (window.opener) {
      window.opener.postMessage({ type: 'oauth2_complete', success: true }, '*');
    }
    setTimeout(() => window.close(), 1500);
    ` : `
    // Notify opener of failure
    if (window.opener) {
      window.opener.postMessage({ type: 'oauth2_complete', success: false, error: ${JSON.stringify(message)} }, '*');
    }
    `}
  </script>
</body>
</html>`;
}
