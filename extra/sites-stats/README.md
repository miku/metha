# stats

```
import fileinput; print(sum((int(line) for line in fileinput.input())))
```

Partial:

```
$ cat ../extra/sites-stats/2025-09-13-size.tsv| awk '$2 ~ /[0-9]/ {print $2}' | paste -sd+ | bc -l
393729890
```
