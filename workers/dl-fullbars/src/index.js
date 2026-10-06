const GITHUB_API = 'https://api.github.com/repos/full-bars/urnetwork-3.23-fix';
const GITHUB_DL = 'https://github.com/full-bars/urnetwork-3.23-fix';

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);

    if (url.pathname === '/latest-version') {
      return getLatestVersion(env, ctx);
    }

    if (url.pathname.startsWith('/releases/download/')) {
      return proxyRelease(request, url);
    }

    return new Response('Not found', { status: 404 });
  }
};

async function getLatestVersion(env, ctx) {
  const cacheKey = 'https://dl.fullbars.xyz/latest-version';
  const cache = caches.default;
  let cached = await cache.match(cacheKey);
  if (cached) return cached;

  const tagName = await resolveLatestTag(env);
  if (!tagName) return new Response('failed', { status: 502 });

  const response = new Response(tagName + '\n', {
    headers: { 'Content-Type': 'text/plain', 'Cache-Control': 'public, max-age=300' }
  });
  ctx.waitUntil(cache.put(cacheKey, response.clone()));
  return response;
}

// Resolve the latest release tag WITHOUT depending on the rate-limited GitHub API.
// api.github.com allows only 60 anonymous requests/hour per source IP, and Workers
// egress from shared Cloudflare IPs, so that budget is exhausted by unrelated
// traffic and the API path returns 403. That made this endpoint answer 502 for
// everyone, which defeats its whole purpose (it exists to rescue clients whose own
// GitHub API call was rate-limited). The /releases/latest web endpoint 302s to the
// tag URL and is not subject to the API rate limit, so it is the primary path.
async function resolveLatestTag(env) {
  // Primary: follow /releases/latest and read the tag off the final URL.
  try {
    const resp = await fetch(`${GITHUB_DL}/releases/latest`, {
      redirect: 'follow',
      headers: { 'User-Agent': 'fullbars-dl-worker' }
    });
    const match = (resp.url || '').match(/\/releases\/tag\/([^/?#]+)/);
    if (match && match[1]) return decodeURIComponent(match[1]);
  } catch (err) {
    // fall through to the API fallback
  }

  // Fallback: the GitHub API. Set the GITHUB_TOKEN secret to raise the anonymous
  // 60/hr ceiling to 5000/hr so a shared egress IP cannot exhaust it.
  try {
    const headers = {
      'User-Agent': 'fullbars-dl-worker',
      'Accept': 'application/vnd.github.v3+json'
    };
    if (env && env.GITHUB_TOKEN) {
      headers['Authorization'] = `Bearer ${env.GITHUB_TOKEN}`;
    }
    const resp = await fetch(`${GITHUB_API}/releases/latest`, { headers });
    if (resp.ok) {
      const data = await resp.json();
      if (data.tag_name) return data.tag_name;
    }
  } catch (err) {
    // fall through
  }

  return null;
}

const FORWARD_REQUEST_HEADERS = ['range', 'if-none-match', 'if-modified-since'];

async function proxyRelease(request, url) {
  const githubUrl = `${GITHUB_DL}${url.pathname}${url.search}`;

  const forwardHeaders = new Headers();
  for (const name of FORWARD_REQUEST_HEADERS) {
    const value = request.headers.get(name);
    if (value) forwardHeaders.set(name, value);
  }
  forwardHeaders.set('User-Agent', 'fullbars-dl-worker');

  let resp;
  try {
    resp = await fetch(githubUrl, { method: request.method, headers: forwardHeaders });
  } catch (err) {
    return new Response('failed', { status: 502 });
  }
  return new Response(resp.body, { status: resp.status, statusText: resp.statusText, headers: resp.headers });
}
