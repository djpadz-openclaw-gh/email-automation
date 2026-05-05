import { NextRequest, NextResponse } from 'next/server';

/**
 * OAuth2 callback handler.
 * 
 * This route intercepts the OAuth2 callback from Google/Microsoft before
 * the Next.js rewrite can proxy it to the backend. It forwards the request
 * to the backend API, then returns an HTML page with JavaScript that:
 * - If opened as a popup: notifies parent via postMessage and closes itself
 * - If opened directly: redirects to home page with success/error
 */
export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ provider: string }> }
) {
  const { provider } = await params;
  const { searchParams } = new URL(request.url);

  const code = searchParams.get('code');
  const state = searchParams.get('state');
  const error = searchParams.get('error');
  const errorDescription = searchParams.get('error_description');

  // If the OAuth provider returned an error, return error page
  if (error) {
    const message = errorDescription || error;
    return getCallbackPage(false, message);
  }

  if (!code || !state) {
    return getCallbackPage(false, 'Missing code or state parameter');
  }

  // Forward the callback to the backend API
  const apiUrl = process.env.API_URL || 'http://api.email-automation.svc.cluster.local:8080';
  const backendUrl = `${apiUrl}/api/oauth2/callback/${encodeURIComponent(provider)}?code=${encodeURIComponent(code)}&state=${encodeURIComponent(state)}`;

  try {
    const response = await fetch(backendUrl, {
      method: 'GET',
      headers: {
        'Content-Type': 'application/json',
      },
    });

    if (!response.ok) {
      const body = await response.json().catch(() => ({ error: response.statusText }));
      const message = body.error || `OAuth failed: ${response.status}`;
      return getCallbackPage(false, message);
    }

    // Success - return page that closes popup or redirects
    return getCallbackPage(true, 'Account connected successfully');
  } catch (err) {
    const message = err instanceof Error ? err.message : 'OAuth callback failed';
    return getCallbackPage(false, message);
  }
}

function getCallbackPage(success: boolean, message: string) {
  const html = `
    <!DOCTYPE html>
    <html>
      <head>
        <title>OAuth Callback</title>
        <script>
          // Check if this window was opened as a popup
          const isPopup = window.opener && !window.opener.closed;
          
          if (isPopup) {
            // Notify parent window via postMessage
            window.opener.postMessage(
              {
                type: 'oauth_callback',
                success: ${success},
                message: ${JSON.stringify(message)}
              },
              '*'
            );
            // Close this popup after a short delay to ensure message is received
            setTimeout(() => window.close(), 100);
          } else {
            // Not a popup - redirect to home page with status in URL
            const param = ${success} ? 'oauth_success=1' : 'oauth_error=' + encodeURIComponent(${JSON.stringify(message)});
            window.location.href = '/?\\' + param;
          }
        </script>
      </head>
      <body>
        <p>${success ? 'Account connected successfully. Closing...' : 'OAuth failed: ' + message}</p>
      </body>
    </html>
  `;
  
  return new NextResponse(html, {
    status: 200,
    headers: {
      'Content-Type': 'text/html; charset=utf-8',
    },
  });
}
