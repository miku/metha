https://ndltd.org/find-etds/

```
cat oatd-repositories.html | pup 'a[href^="http"] attr{href}' | sed -e 's@?.*@@g' | sort -u
```
