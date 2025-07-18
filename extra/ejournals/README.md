https://ejournals.eu/api-front/en/journals

Load, inspect network, find "json", copy as curl


```
curl 'https://ejournals.eu/api-front/en/journals' \
  --compressed \
  -X POST \
  -H 'User-Agent: Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0' \
  -H 'Accept: */*' \
  -H 'Accept-Language: en-US,en;q=0.5' \
  -H 'Accept-Encoding: gzip, deflate, br, zstd' \
  -H 'Referer: https://ejournals.eu/en/journals-list' \
  -H 'Content-Type: application/json' \
  -H 'Origin: https://ejournals.eu' \
  -H 'Connection: keep-alive' \
  -H 'Sec-Fetch-Dest: empty' \
  -H 'Sec-Fetch-Mode: cors' \
  -H 'Sec-Fetch-Site: same-origin' \
  -H 'Priority: u=4' \
  -H 'Pragma: no-cache' \
  -H 'Cache-Control: no-cache' \
  -H 'TE: trailers' \
  --data-raw '{"sort":"TITLE_ASC","template":"grid","text":"","domain":[],"partners":[],"meinPointsMin":null,"meinPointsMax":null,"licence":[],"state":[],"type":[],"periodicity":[],"indexation":[],"language":[]}' > journals.txt
```

Guess endpoints:

```
jq . journals.txt | grep -o '/en/journal/[^"\\]*' | awk '{print "https://ejournals.eu"$0"/oai" }' > oai.txt
```
