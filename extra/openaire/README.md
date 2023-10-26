# Openaire

```shell
$ cat data-2000.tsv | \
    cut -f 5 | \
    grep 'index.php' | \
    sed -e 's@/index$@@' | \
    awk '{print $0"/oai"}'
```
