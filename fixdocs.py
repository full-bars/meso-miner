import re, sys

changed = []

# 1. config table row -> retired
p = 'docs/Configuration.md'
s = open(p).read()
old = '| `ENABLE_VNSTAT` | `true` | Enables the traffic monitor on port 8080. |'
if old in s:
    s = s.replace(old, '| `ENABLE_VNSTAT` | none | Retired. Accepted so old compose files keep working; it starts nothing and logs a warning. Use the `/metrics` endpoint on `:9091` instead. |')
    open(p, 'w').write(s)
    changed.append(p)

# 2. deployment page: replace the how-to body, keep the heading (README anchors it)
p = 'docs/Docker-Deployment.md'
lines = open(p).read().split('\n')
starts = [i for i, l in enumerate(lines) if l.startswith('## ') and 'vnStat' in l]
if starts:
    st = starts[0]
    en = next((i for i in range(st + 1, len(lines)) if lines[i].startswith('## ')), len(lines))
    lines[st:en] = [
        '## 📊 (Optional) Running with vnStat Bandwidth Monitor',
        '',
        'vnStat is retired. It is no longer built into the container, and `ENABLE_VNSTAT=true` now only prints a warning. Nothing listens on port 8080, and there is no `/var/lib/vnstat` volume to mount.',
        '',
        'For traffic telemetry use the Prometheus `/metrics` endpoint on port `9091`, which starts by default.',
        '',
    ]
    changed.append(p)

flags = [
    r'-e\s+ENABLE_VNSTAT=true\s*\\?',
    r'-v\s+\S*vnstat\S*:/var/lib/vnstat\s*\\?',
    r'-p\s+\S*:8080\s*\\?',
    r'- ENABLE_VNSTAT=true',
    r'- \S*vnstat\S*:/var/lib/vnstat.*',
    r'- "\d+:8080"',
    r'[A-Za-z0-9_.-]+_vnstat:.*',
]
kept = []
dropped = 0
for ln in lines:
    t = ln.strip()
    if any(re.fullmatch(f, t) for f in flags):
        dropped += 1
        continue
    kept.append(ln)
open(p, 'w').write('\n'.join(kept))

# 3. scaling guide: same strip, and the prose that promised a vnStat volume
p = 'docs/Multi-Container-Scaling.md'
lines = open(p).read().split('\n')
kept = []
d2 = 0
for ln in lines:
    t = ln.strip()
    if any(re.fullmatch(f, t) for f in flags):
        d2 += 1
        continue
    kept.append(ln)
s = '\n'.join(kept)
s = s.replace('Each service needs a unique service name, container name, host port, and vnStat volume.',
              'Each service needs a unique service name and container name.')
open(p, 'w').write(s)
changed.append(p)

print('changed files:', changed)
print('docker-run/compose lines dropped:', dropped, 'scaling lines dropped:', d2)

# validate every fenced yaml block still parses
try:
    import yaml
    for page in ('docs/Multi-Container-Scaling.md', 'docs/Docker-Deployment.md'):
        blocks = re.findall(r'```yaml\n(.*?)```', open(page).read(), re.S)
        bad = 0
        for b in blocks:
            try:
                yaml.safe_load(b)
            except Exception:
                bad += 1
        print(f'{page}: {len(blocks)} yaml blocks, {bad} invalid')
except ImportError:
    print('yaml module unavailable, skipped block validation')
