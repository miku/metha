# metha-migrate-041

Migrate cached files to zstd. The default compression is gzip, but zstd is
faster to decompress.

```
$ make
$ ./metha-migrate-041
```

With options:

```
$ ./metha-migrate-041 -l 6 -w 32 -f -B
```
