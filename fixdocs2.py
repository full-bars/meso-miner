import re

# README pointer: keep the link target (the heading survives), fix the promise
p = 'README.md'
s = open(p).read()
old = "> Providers do not require any listening ports. Outbound connections handle all traffic. If you wish to enable the optional vnstat web monitor, see [Running with vnstat](docs/Docker-Deployment.md#-optional-running-with-vnstat-bandwidth-monitor)."
new = "> Providers do not require any listening ports. Outbound connections handle all traffic. vnStat is retired and `ENABLE_VNSTAT` now only warns; for traffic telemetry use the Prometheus `/metrics` endpoint on `:9091`. See [vrunning with vnstat](docs/Docker-Deployment.md#-optional-running-with-vnstat-bandwidth-monitor) for the retirement note."
if old in s:
    s = s.replace(old, new)
    open(p, 'w').write(s)
    print('README updated')
else:
    print('README sentence not matched')

# Pelican page bullet
p = 'pelican/README.md'
s = open(p).read()
m = re.search(r'- vnStat ships OFF by default:[^\n]*\n', s)
if m:
    s = s[:m.start()] + '- vnStat is retired: the variable is still accepted so old eggs keep importing, but it starts nothing and logs a warning. Port 8080 is not exposed.\n' + s[m.end():]
    open(p, 'w').write(s)
    print('pelican README updated')

# Shakedown W4: invert the assertion, a retired feature must NOT serve 8080
p = '.github/scripts/docker-shakedown.sh'
s = open(p).read()
old = '  [ "$HTTP" != "000" ] && [ -n "$HTTP" ] && ok "W4 vnstat port 8080 serves (HTTP $HTTP)" || bad "W4 vnstat port 8080 did not serve (HTTP ${HTTP:-none})"'
new = '  [ "$HTTP" = "000" ] && ok "W4 vnstat stays retired, port 8080 serves nothing" || bad "W4 vnstat port 8080 answered (HTTP $HTTP) although vnStat is retired"'
if old in s:
    s = s.replace(old, new)
    open(p, 'w').write(s)
    print('shakedown W4 inverted')
else:
    print('shakedown assertion not matched')
