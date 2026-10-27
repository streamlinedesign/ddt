#!/usr/bin/env bash
# Cut Geist down to the letters the print uses (English and Croatian, with
# typographic punctuation) and to the weights the CSS asks for. Needs: pip install fonttools brotli
# Usage: tools/fonts.sh   (reads design/fonts, writes web/static/fonts)
set -euo pipefail
cd "$(dirname "$0")/.."
in=design/fonts out=web/static/fonts
mkdir -p "$out"
latin="U+0020-007E,U+00A0-00FF,U+0131,U+0152-0153,U+02C6,U+02DA,U+02DC,U+2013-2014,U+2018-201E,U+2022,U+2026,U+2032-2033,U+2039-203A,U+20AC,U+2122,U+2190-2193,U+2212"
croatian="U+0106-0107,U+010C-010D,U+0110-0111,U+0160-0161,U+017D-017E"
features="kern,liga,lnum,tnum,case,calt"
pyftsubset "$in/Geist-Variable.woff2" --unicodes="$latin,$croatian" --layout-features="$features" --flavor=woff2 --output-file="$out/geist.woff2"
cp "$in"/LICENSE-Geist.txt "$out/"
ls -l "$out" | awk 'NR>1 {s+=$5} END {print "fonts:", int(s/1024), "KB"}'
