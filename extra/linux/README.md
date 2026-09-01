# Running metha on a timer

`metha sweep` harvests every endpoint it knows about that is due, records what
became of each one, and exits. It is a batch command, not a daemon, so a systemd
timer is all the scheduling it needs — and restarts, logging, resource limits
and journal history stay where an operator already looks for them.

Two files:

| | |
|---|---|
| `metha.service` | one sweep, `Type=oneshot` |
| `metha.timer` | daily, persistent, with two hours of jitter |

## Install

```shell
$ sudo useradd --system --home-dir /var/lib/metha --shell /usr/sbin/nologin metha
$ sudo install -m644 metha.service metha.timer /etc/systemd/system/
$ sudo systemctl daemon-reload
$ sudo systemctl enable --now metha.timer
```

The timer is what gets enabled; the service has no `[Install]` section, because
a sweep wanted by `multi-user.target` would also run at every boot.

Adjust `ExecStart` if metha is not in `/usr/local/bin`, and `METHA_DIR` if the
cache belongs somewhere other than `/var/lib/metha`. `StateDirectory=metha`
creates that directory with the right ownership on first start, so there is
nothing to `mkdir`.

To run it under your own account instead, drop the two files in
`~/.config/systemd/user/`, remove `User=`, `Group=`, `StateDirectory=` and
`ProtectHome=`, point `METHA_DIR` at `%h/.cache/metha`, and use
`systemctl --user enable --now metha.timer`. A user timer only runs while you
are logged in unless you `loginctl enable-linger`.

## Watching it

```shell
$ systemctl list-timers metha.timer     # when the next sweep is
$ systemctl status metha.service        # what the last one did
$ journalctl -u metha.service -n 40     # the summary it left behind
$ sudo systemctl start metha.service    # sweep now, without waiting
```

The last few lines of each run are the report worth reading:

```
244,040 in the roster, 242,796 due, 1,244 held back
238,880 ok, 3,102 empty, 891 transient, 203 protocol, 25 gone (2,140,882 new records)
1,144 changed state: 41 to probation, 12 to quarantined, 3 recovered
242,796 swept in 6h14m
```

A run of endpoints changing state overnight is how a network problem, a blocked
user agent or a bad release announces itself.

The progress counter writes one line every thirty seconds into the journal,
which is the liveness signal; `--quiet` turns it off if you would rather have
only the summary.

## Two settings worth understanding before changing them

**`--budget` must be comfortably shorter than the timer interval.** With a daily
timer, `--budget 20h` leaves four hours of headroom. A sweep that runs past the
next fire is not a failure — the sweep lock means the second one prints
`another sweep is running, nothing to do` and exits 0, so the timer neither
flaps nor mails anybody — but an installation permanently one sweep behind is
worth noticing rather than absorbing.

**`TimeoutStartSec=infinity` is there for the next person, not for today.** For
`Type=oneshot` the start timeout is already disabled by default, so the line
changes nothing as the unit stands. It matters if `Type=` is ever changed:
everywhere else that setting bounds the whole of `ExecStart` and takes ninety
seconds from `DefaultTimeoutStartSec`, which would cut every sweep a minute and
a half in — the same mistake, `RuntimeMaxSec=300s`, that this unit exists to
correct, and made without touching a line that mentions time.

## What replaced what

The unit that used to live here was:

```ini
ExecStart=/bin/bash -c 'metha sync --list | shuf | parallel -j 64 -I {} "metha sync --base-dir $HOME/.cache/metha {}"'
RuntimeMaxSec=300s
Restart=always
```

It harvested 326M records, so the bar was never "does it work". What a sweep
knows that the pipeline could not:

- **It remembers.** `shuf` re-draws a fresh permutation on every restart, so
  covering the corpus is a coupon-collector problem — about twelve times the
  requests a single ordered pass needs. The roster is a pass.
- **The dead get cheaper.** A URL that has never resolved cost a worker slot for
  minutes, on every pass, forever. It now costs about five requests a year, and
  is never dropped: repositories move and domains come back.
- **Long harvests finish.** No endpoint is cut at five minutes any more; the
  per-endpoint bound is `--deadline`, an hour by default.
- **One request per host at a time.** 244,346 endpoints live on 62,294 hosts,
  and 146 hosts hold a hundred or more. The pool partitions by host, so a large
  repository is never asked several questions at once.

`metha endpoints` is the view onto what it learned: `--state quarantined` for
what has stopped answering, `--class gone` for what never answered at all,
`--slower-than 5m --json` for what a sweep spends its time on, and
`--state active` for the corrected endpoint list.
