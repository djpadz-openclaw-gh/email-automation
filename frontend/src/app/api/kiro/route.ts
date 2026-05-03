import { NextRequest, NextResponse } from 'next/server';

const KIRO_API_URL =
  process.env.KIRO_API_URL ||
  'http://kiro-gateway.kiro-gateway.svc.cluster.local:9000/v1/messages';

const KIRO_API_KEY = process.env.KIRO_API_KEY || process.env.KIRO_GATEWAY_TOKEN || '';

export async function POST(request: NextRequest) {
  try {
    const body = await request.json();

    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };

    if (KIRO_API_KEY) {
      headers['x-api-key'] = KIRO_API_KEY;
    }

    const res = await fetch(KIRO_API_URL, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
    });

    if (!res.ok) {
      const errorBody = await res.text().catch(() => res.statusText);
      return NextResponse.json(
        { error: `Kiro API error ${res.status}: ${errorBody}` },
        { status: res.status }
      );
    }

    const data = await res.json();
    return NextResponse.json(data);
  } catch (err) {
    const message = err instanceof Error ? err.message : 'Unknown error';
    return NextResponse.json({ error: message }, { status: 500 });
  }
}
