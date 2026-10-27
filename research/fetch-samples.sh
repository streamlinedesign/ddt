#!/usr/bin/env bash
# Saves one real sample of every Tisak source into ./samples/ and zips it.
# Usage:
#   export TMDB_API_KEY=...   # your TMDB key
#   bash fetch-samples.sh
# Upload samples.zip in the thread. The saved answers never contain your keys,
# and the log below prints addresses with the keys masked.
set -u
OUT=samples
mkdir -p "$OUT"
LOG="$OUT/_log.txt"
: > "$LOG"
UA="Mozilla/5.0 (X11; Linux x86_64) Tisak-research"

get() { # get <file> <url>
  local file="$1" url="$2" code
  code=$(curl -sL -m 30 -A "$UA" -o "$OUT/$file" -w "%{http_code}" "$url")
  local shown="${url//${TMDB_API_KEY:-__none__}/KEY}"
  printf "%s  %-32s %8s bytes  %s\n" "$code" "$file" "$(wc -c < "$OUT/$file")" "$shown" | tee -a "$LOG"
  sleep 1
}

# News feeds
get hrt-hrvatska.xml          https://feed.hrt.hr/vijesti/hrvatska.xml
get hrt-svijet.xml            https://feed.hrt.hr/vijesti/svijet.xml
get hrt-eu.xml                https://feed.hrt.hr/vijesti/eu.xml
get hrt-ostali-sportovi.xml   https://feed.hrt.hr/sport/ostali-sportovi.xml
get hrt-hrvatski-nogomet.xml  https://feed.hrt.hr/sport/hrvatski-nogomet.xml
get hrt-medunarodni-nogomet.xml https://feed.hrt.hr/sport/medunarodni-nogomet.xml
get hrt-kosarka.xml           https://feed.hrt.hr/sport/kosarka.xml
get hrt-rukomet.xml           https://feed.hrt.hr/sport/rukomet.xml
get hrt-znanost-tehnologija.xml https://feed.hrt.hr/magazin/znanost-i-tehnologija.xml
get hrt-kultura.xml           https://feed.hrt.hr/magazin/kultura.xml
for i in 0 1 2 3 4 5; do get germanijak-$i.xml https://www.germanijak.hr/rss/$i.xml; done
get bug.xml                   https://www.bug.hr/rss
get hn-frontpage.xml          https://hnrss.org/frontpage
get loudwire.xml              https://loudwire.com/feed/
get glazba.xml                https://glazba.hr/feed/
get muzika.xml                https://www.muzika.hr/feed/
get kinofilm.xml              https://kinofilm.hr/feed/

# Weather
get meteo-7d.html             "https://meteo.hr/prognoze.php?id=prognoza&section=prognoze_model&param=7d&Code=ZAGREB-GRI%C4%8C"

# NASA picture of the day: the keyless APOD Basic feed (the old api.nasa.gov one answers 500)
get apod-basic.json           "https://science.nasa.gov/wp-json/wp/v2/apod-basic?per_page=3"
get apod-basic.rss            https://science.nasa.gov/feed/apod-basic/

# TMDB (Croatian releases and a film with its release dates)
if [ -n "${TMDB_API_KEY:-}" ]; then
  get tmdb-upcoming-hr.json   "https://api.themoviedb.org/3/movie/upcoming?region=HR&language=hr-HR&api_key=$TMDB_API_KEY"
  get tmdb-discover-hr.json   "https://api.themoviedb.org/3/discover/movie?region=HR&with_release_type=2|3&release_date.gte=$(date +%F)&release_date.lte=$(date -d '+60 days' +%F)&sort_by=primary_release_date.asc&language=hr-HR&api_key=$TMDB_API_KEY"
  get tmdb-movie.json         "https://api.themoviedb.org/3/movie/693134?language=hr-HR&append_to_response=release_dates&api_key=$TMDB_API_KEY"
  get tmdb-search.json        "https://api.themoviedb.org/3/search/movie?query=dune&language=hr-HR&api_key=$TMDB_API_KEY"
  get tmdb-configuration.json "https://api.themoviedb.org/3/configuration?api_key=$TMDB_API_KEY"
else
  echo "TMDB_API_KEY not set: skipped TMDB" | tee -a "$LOG"
fi

# Sport (TheSportsDB free key; 133912 = Croatia)
get sdb-team.json             "https://www.thesportsdb.com/api/v1/json/123/searchteams.php?t=Croatia"
get sdb-last.json             "https://www.thesportsdb.com/api/v1/json/123/eventslast.php?id=133912"
get sdb-next.json             "https://www.thesportsdb.com/api/v1/json/123/eventsnext.php?id=133912"
EVENT=$(grep -o '"idEvent":"[0-9]*"' "$OUT/sdb-last.json" | head -1 | grep -o '[0-9]*')
[ -n "$EVENT" ] && get sdb-timeline.json "https://www.thesportsdb.com/api/v1/json/123/lookuptimeline.php?id=$EVENT"

# Billboard top 10 (public copies of the charts)
get billboard-hot100.json     https://raw.githubusercontent.com/mhollingshead/billboard-hot-100/main/recent.json
get billboard-200.csv         https://raw.githubusercontent.com/utdata/rwd-billboard-data/main/data-out/billboard-200-current.csv
# that file holds every week since 1967 (35 MB): keep only the newest week
NEWEST=$(cut -d, -f1 "$OUT/billboard-200.csv" | grep -E '^[0-9]{4}-' | sort | tail -1)
{ head -1 "$OUT/billboard-200.csv"; grep "^$NEWEST," "$OUT/billboard-200.csv"; } > "$OUT/b200.tmp" && mv "$OUT/b200.tmp" "$OUT/billboard-200.csv"

# Theatre schedules
get theatre-komedija.html     https://www.komedija.hr/www/raspored-predstava/
get theatre-kerempuh.html     https://kazalistekerempuh.hr/raspored/
get theatre-hnk.html          https://www.hnk.hr/hr/raspored/
get theatre-scenoteka.html    https://scenoteka.hr/raspored/
get theatre-ludakuca.html     https://www.ludakuca.hr/raspored/
get theatre-aplauz.html       https://aplauz-teatar.hr/raspored.html
get theatre-exit.html         https://teatarexit.hr/raspored
get theatre-gavella.html      https://www.gavella.hr/raspored-izvedbi
get theatre-knap.html         https://www.knap.hr/kazaliste/raspored-predstava/

zip -qr samples.zip "$OUT" && echo "Done: upload samples.zip" || echo "Done: zip the samples folder and upload it"
echo "Anything not starting with 200 in $LOG is worth a look."
