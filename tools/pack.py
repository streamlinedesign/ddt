#!/usr/bin/env python3
"""Pack the static design into one self-contained HTML file, the way the press will pack an edition.

Usage: python3 tools/pack.py design/index.html design/standalone.html

Stylesheets, scripts, fonts, icons and local pictures go inline as text or data URIs.
Remote pictures (film posters, theatre photos) stay as links here; the press saves them next to the edition.
"""
import base64, mimetypes, os, re, sys

src, dst = sys.argv[1], sys.argv[2]
root = os.path.dirname(os.path.abspath(src))
mimetypes.add_type('font/woff2', '.woff2')
mimetypes.add_type('image/svg+xml', '.svg')
mimetypes.add_type('image/webp', '.webp')

def local(path, base=root):
    if re.match(r'^(https?:|data:|#|//)', path):
        return None
    full = os.path.normpath(os.path.join(base, path))
    return full if os.path.isfile(full) else None

def data_uri(full):
    kind = mimetypes.guess_type(full)[0] or 'application/octet-stream'
    with open(full, 'rb') as f:
        return 'data:%s;base64,%s' % (kind, base64.b64encode(f.read()).decode())

def css_urls(css, base):
    def sub(m):
        full = local(m.group(2), base)
        return 'url(%s)' % (data_uri(full) if full else m.group(1) + m.group(2) + m.group(1))
    return re.sub(r'url\(\s*([\'"]?)([^\'")]+)\1\s*\)', sub, css)

html = open(src, encoding='utf-8').read()

# font preloads are pointless once fonts are inline
html = re.sub(r'<link rel="preload"[^>]*>\s*', '', html)

def stylesheet(m):
    full = local(m.group(1))
    if not full:
        return m.group(0)
    css = open(full, encoding='utf-8').read()
    return '<style>\n%s\n</style>' % css_urls(css, os.path.dirname(full))
html = re.sub(r'<link rel="stylesheet" href="([^"]+)">', stylesheet, html)

def script(m):
    full = local(m.group(1))
    if not full:
        return m.group(0)
    return '<script>\n%s\n</script>' % open(full, encoding='utf-8').read().replace('</script', '<\\/script')
html = re.sub(r'<script src="([^"]+)"></script>', script, html)

def attr(m):
    full = local(m.group(3))
    return m.group(1) + '=' + m.group(2) + (data_uri(full) if full else m.group(3)) + m.group(2)
html = re.sub(r'\b(src|data-play)=(["\'])([^"\']+)\2', attr, html)
html = re.sub(r'style="([^"]*url\([^"]*)"', lambda m: 'style="%s"' % css_urls(m.group(1).replace('&quot;', '"'), root), html)

open(dst, 'w', encoding='utf-8').write(html)
left = sorted(set(re.findall(r'(?:src|href)="((?!https?:|data:|#|mailto:)[^"]+)"', html)))
print('%s: %d KB' % (dst, os.path.getsize(dst) // 1024))
print('local references left:', left or 'none')
