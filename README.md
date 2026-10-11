# dump1090-cli

[![CI](https://github.com/technothatch/dump1090-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/technothatch/dump1090-cli/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/technothatch/dump1090-cli/blob/main/LICENSE)
[![Latest Release](https://img.shields.io/github/v/release/technothatch/dump1090-cli)](https://github.com/technothatch/dump1090-cli/releases)
[![Issues](https://img.shields.io/github/issues/technothatch/dump1090-cli)](https://github.com/technothatch/dump1090-cli/issues)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](https://github.com/technothatch/dump1090-cli/pulls)
[![Code size](https://img.shields.io/github/languages/code-size/technothatch/dump1090-cli)](https://github.com/technothatch/dump1090-cli)
[![Last commit](https://img.shields.io/github/last-commit/technothatch/dump1090-cli)](https://github.com/technothatch/dump1090-cli/commits/main)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8.svg)](https://go.dev/doc/devel/release)
[![Coverage](https://img.shields.io/badge/coverage-91%25-brightgreen.svg)](https://github.com/technothatch/dump1090-cli)

**dump1090-cli** turns the JSON documents served by
[dump1090-fa](https://github.com/flightaware/dump1090) and
[readsb](https://github.com/wiedehopf/readsb) into fast, readable terminal
tables: what is in the air around you, who is closest, which transponder
codes are in use, and whether anybody has declared an emergency. It replaces
the habit of `curl …/data/aircraft.json | jq` with sorted, aligned,
colour-coded views that work over SSH, in scripts and over a serial link to a
receiver running `--write-json`. One static binary, standard library only —
no cobra, no tview, no runtime to install.

## Features

- **Eight commands**: `ls`, `watch`, `info HEX`, `nearest LAT LON`,
  `box LAT1 LAT2 LON1 LON2`, `stats`, `squawks`, `emergency`.
- **Auto-discovery**: probes `http://localhost:8080` (dump1090-fa/readsb HTTP
  port), then `:8081`, then the readsb API port `:8042`; `--url` and
  `DUMP1090_URL` override it.
- **Path fallbacks**: `/data/aircraft.json` → `/aircraft.json` → `/`, so the
  readsb API port works without configuration.
- **`--json` on every command**: `ls`/`box`/`emergency` emit an
  `aircraft.json`-shaped document with the decoder's original elements
  verbatim, `info` emits the raw object, `stats` emits the raw
  `stats.json` bytes, `nearest` adds the query `point`, `watch` streams
  NDJSON.
- **Distance that makes sense**: the decoder's own `dst`/`dir` fields win,
  otherwise the distance is computed from `receiver.json` with the
  haversine formula; `nearest` re-ranks everything against the point you
  typed.
- **Colour when it helps, never in pipes**: hex addresses cyan, callsigns
  bold, distances green, ground targets yellow, emergency squawks red — and
  plain text whenever stdout is redirected or `NO_COLOR` is set.
- **Imperial by default, `--metric` for metres, km/h and kilometres.**
- **Ground targets handled properly**: `alt_baro: "ground"` renders as a
  yellow `GND`, not `0`.
- **Emergency awareness**: `emergency` matches squawk 7500/7600/7700 *or*
  any non-`none` emergency classification, because decoders often set only
  one of the two.
- **Graceful failures**: `exit 0` success, `exit 1` decoder error or
  aircraft not found, `exit 2` usage error — plus a one-line hint listing
  the URLs auto-discovery tried when no decoder answers.

## Install

Requires Go 1.25 or newer.

```sh
go install github.com/technothatch/dump1090-cli/cmd/dump1090-cli@latest
```

or build from source:

```sh
git clone https://github.com/technothatch/dump1090-cli.git
cd dump1090-cli
go build ./...
./dump1090-cli --help        # Windows: .\dump1090-cli.exe --help
```

## Quickstart

Every output below is real output from the binary, served by a local
readsb-shaped fixture on `http://127.0.0.1:8099`.

### `ls` — what is in the air, closest to the receiver first

```console
$ dump1090-cli ls --url http://127.0.0.1:8099
HEX      CALLSIGN  TYPE  ALT(ft)  GS(kt)  TRACK  SQUAWK  DST(nm)
3c7d1e   SWR13F    A333    24500     382    144    7700     11.6
4b1e7f   ADAC5     H145     2500      96     22    7500     12.3
4406e8   DLH16A    A320      GND      14    245    7000     14.0
4406cb   EWG6K     A319    31025     441    271    7600     21.6
~0a0f49  -         -       12000     180     45    1200     31.2
4406c2   -         A388    39000     488    204    1013     38.6
3c673a   DLH441    A20N    34975     462     95    1013     56.5
4b18d5   IFY230    C680    17000       -      -    7000        -
```

Sorted by distance; aircraft with no known position (`4b18d5`) come last,
`GND` is a ground target, `~0a0f49` is a non-ICAO (TIS-B) address and `-`
means the decoder did not report that field.

```console
$ dump1090-cli ls --metric --limit 4 --url http://127.0.0.1:8099
HEX     CALLSIGN  TYPE  ALT(m)  GS(km/h)  TRACK  SQUAWK  DST(km)
3c7d1e  SWR13F    A333    7468       707    144    7700     21.5
4b1e7f  ADAC5     H145     762       178     22    7500     22.8
4406e8  DLH16A    A320     GND        26    245    7000     26.0
4406cb  EWG6K     A319    9456       817    271    7600     40.1
```

### `info` — every known field of one aircraft

```console
$ dump1090-cli info 3c673a --url http://127.0.0.1:8099
hex                3c673a
callsign           DLH441
type               A20N
registration       D-AIEA
category           A3
squawk             1013
altitude           34975 ft
geom altitude      35125 ft
ground speed       462 kt
indicated airspeed 266 kt
true airspeed      471 kt
mach               0.812
track              95.3 deg
baro rate          -64 ft/min
geom rate          -96 ft/min
position           48.9123, 12.4567
dst                56.5 nm from receiver
dir                41 deg from receiver
rssi               -14.2 dBFS
messages           2103
seen               0.4 s
seen pos           0.7 s
```

Absent fields are omitted entirely, so a sparse Mode-S target stays short.

### `nearest` — rank everything against a point

```console
$ dump1090-cli nearest 48.2 11.5 --url http://127.0.0.1:8099
HEX      CALLSIGN  TYPE  ALT(ft)  GS(kt)  TRACK  SQUAWK  DST(nm)
3c7d1e   SWR13F    A333    24500     382    144    7700     11.5
4b1e7f   ADAC5     H145     2500      96     22    7500     12.9
4406e8   DLH16A    A320      GND      14    245    7000     14.7
4406cb   EWG6K     A319    31025     441    271    7600     21.0
~0a0f49  -         -       12000     180     45    1200     31.2
4406c2   -         A388    39000     488    204    1013     38.0
3c673a   DLH441    A20N    34975     462     95    1013     57.2
```

### `box` — aircraft inside a bounding box

Corner order does not matter; edges are inclusive.

```console
$ dump1090-cli box 47.5 48.5 10.5 12.5 --url http://127.0.0.1:8099
HEX     CALLSIGN  TYPE  ALT(ft)  GS(kt)  TRACK  SQUAWK  DST(nm)
3c7d1e  SWR13F    A333    24500     382    144    7700     11.6
4b1e7f  ADAC5     H145     2500      96     22    7500     12.3
4406e8  DLH16A    A320      GND      14    245    7000     14.0
4406cb  EWG6K     A319    31025     441    271    7600     21.6
4406c2  -         A388    39000     488    204    1013     38.6
```

### `emergency` — squawk 7500/7600/7700 or a non-`none` classification

```console
$ dump1090-cli emergency --url http://127.0.0.1:8099
HEX     CALLSIGN  SQUAWK  EMERGENCY  ALT(ft)  GS(kt)  DST(nm)
3c7d1e  SWR13F      7700  general      24500     382     11.6
4b1e7f  ADAC5       7500  lifeguard     2500      96     12.3
4406cb  EWG6K       7600  -            31025     441     21.6
```

`4406cb` broadcasts the radio-failure squawk without setting the emergency
field, and still shows up — both signals count.

### `squawks` — Mode A codes in use

```console
$ dump1090-cli squawks --url http://127.0.0.1:8099
SQUAWK  COUNT  AIRCRAFT
1013        2  3c673a, 4406c2
7000        2  4406e8, 4b18d5
1200        1  ~0a0f49
7500        1  4b1e7f
7600        1  4406cb
7700        1  3c7d1e
8 aircraft, 8 with squawk, 6 distinct squawks
```

### `stats` — one row per metric, one column per window

```console
$ dump1090-cli stats --url http://127.0.0.1:8099
METRIC                 latest  last1min  last5min  last15min      total
messages                  118       611      3042       9077    4711023
blocks_processed           44       231      1157       3468     892140
blocks_dropped              0         0         0          1          3
modes                     115       594      2954       8813    4598633
modeac                      3        17        88        264     112390
bad                         0         2         9         27      12441
unknown_icao                1         4        21         63       3221
signal                  -14.8     -14.4     -14.1      -13.9      -14.2
peak_signal              -3.2      -2.7      -2.1       -1.6       -1.0
strong_signals             12        63       318        951      45211
cpu.demod_ms             41.2     214.6    1071.4     3214.7  1240088.5
cpu.reader_ms             8.9      47.1     236.8      710.2    40211.7
cpu.background_ms        12.4      66.2     331.5      994.6    90123.3
cpr.global_ok              12        58       291        873      41230
cpr.global_bad              0         1         3          9         12
cpr.global_skipped          2         9        46        138        891
cpr.local_ok               34       177       884       2651     118402
cpr.local_bad               1         3        12        35         44
cpr.local_skipped           5        24       119       357       3102
tracks.all                  8        11        14         16          8
tracks.single_message       2         3         4          5          1
```

### `watch` — the live view

```console
$ dump1090-cli watch --url http://127.0.0.1:8099
16:11:15  8 aircraft  4711023 messages  http://127.0.0.1:8099
HEX      CALLSIGN  TYPE  ALT(ft)  GS(kt)  TRACK  SQUAWK  DST(nm)
3c7d1e   SWR13F    A333    24500     382    144    7700     11.6
4b1e7f   ADAC5     H145     2500      96     22    7500     12.3
4406e8   DLH16A    A320      GND      14    245    7000     14.0
4406cb   EWG6K     A319    31025     441    271    7600     21.6
~0a0f49  -         -       12000     180     45    1200     31.2
4406c2   -         A388    39000     488    204    1013     38.6
3c673a   DLH441    A20N    34975     462     95    1013     56.5
4b18d5   IFY230    C680    17000       -      -    7000        -
```

Refreshes every `--interval` (default 1s) until Ctrl-C. On a terminal the
frame is repainted in place (cursor hidden, `ESC[2J` clear); piped or
redirected output gets one frame after another separated by a blank line.

### `--json` — the same data for scripts

```console
$ dump1090-cli info 3c7d1e --json --url http://127.0.0.1:8099
{
  "hex": "3c7d1e",
  "type": "A333",
  "flight": "SWR13F  ",
  "r": "HB-JCA",
  "category": "A3",
  "squawk": "7700",
  "emergency": "general",
  "alt_baro": 24500,
  "alt_geom": 24650,
  "gs": 381.5,
  "ias": 243,
  "tas": 392,
  "mach": 0.641,
  "track": 143.7,
  "baro_rate": -1280,
  "geom_rate": -1344,
  "lat": 48.05,
  "lon": 11.68,
  "nic": 9,
  "rc": 93,
  "seen_pos": 0.4,
  "seen": 0.2,
  "messages": 320,
  "rssi": -9.6
}
```

`ls --json` wraps the same elements in an `aircraft.json`-shaped document
(`now`, `messages`, `aircraft[]`), so `dump1090-cli ls --json | jq '.aircraft[].hex'`
works with existing tooling:

```console
$ dump1090-cli ls --json --limit 2 --url http://127.0.0.1:8099
{
  "now": 1759500000.5,
  "messages": 4711023,
  "aircraft": [
    {
      "hex": "3c7d1e",
      "type": "A333",
      "flight": "SWR13F  ",
      "r": "HB-JCA",
      "category": "A3",
      "squawk": "7700",
      "emergency": "general",
      "alt_baro": 24500,
      "alt_geom": 24650,
      "gs": 381.5,
      "ias": 243,
      "tas": 392,
      "mach": 0.641,
      "track": 143.7,
      "baro_rate": -1280,
      "geom_rate": -1344,
      "lat": 48.05,
      "lon": 11.68,
      "nic": 9,
      "rc": 93,
      "seen_pos": 0.4,
      "seen": 0.2,
      "messages": 320,
      "rssi": -9.6
    },
    {
      "hex": "4b1e7f",
      "type": "H145",
      "flight": "ADAC5   ",
      "r": "D-HNFD",
      "category": "A1",
      "squawk": "7500",
      "emergency": "lifeguard",
      "alt_baro": 2500,
      "alt_geom": 2575,
      "gs": 96.2,
      "track": 21.5,
      "baro_rate": 640,
      "geom_rate": 640,
      "lat": 48.4,
      "lon": 11.62,
      "seen_pos": 0.3,
      "seen": 0.2,
      "messages": 654,
      "rssi": -11.1
    }
  ]
}
```

### When no decoder answers

```console
$ dump1090-cli ls
dump1090-cli: ls: dump1090: fetch http://localhost:8080/data/aircraft.json: Get "http://localhost:8080/data/aircraft.json": dial tcp [::1]:8080: connectex: No connection could be made because the target machine actively refused it.
dump1090-cli: auto-discovery checked http://localhost:8080, http://localhost:8081, http://localhost:8042
dump1090-cli: is dump1090/readsb running? Try --url http://HOST:8080
$ echo $?
1
```

The extra two lines appear only when neither `--url` nor `DUMP1090_URL`
decided the base URL.

## Configuration reference

Flags may appear before or after the command and its arguments; `--` stops
flag parsing (useful for negative coordinates); `-`-prefixed tokens that
parse as numbers are treated as positionals.

| Flag / env | Meaning | Default |
|---|---|---|
| `--url URL` | Decoder base origin, e.g. `http://readsb.lan:8080` | `$DUMP1090_URL`, else auto-discover |
| `DUMP1090_URL` | Same as `--url`, from the environment | unset |
| `--interval DUR` | `watch` refresh interval (Go duration, e.g. `500ms`) | `1s` |
| `--timeout DUR` | HTTP timeout per request | `5s` |
| `--json` | Emit JSON instead of a table (every command) | off |
| `--metric` | Metres, km/h, kilometres instead of ft, kt, nm | off (imperial) |
| `--limit N` | Maximum rows printed (`0` = all) | all rows; `nearest` defaults to 10 |
| `-h`, `--help` | Print usage | — |
| `NO_COLOR` | Any non-empty value disables colour | colour on terminals only |

Auto-discovery order (only consulted when neither `--url` nor
`DUMP1090_URL` is set):

| Order | Base URL | Why |
|---|---|---|
| 1 | `http://localhost:8080` | dump1090-fa and readsb HTTP default |
| 2 | `http://localhost:8081` | common alternative HTTP port (tar1090 setups) |
| 3 | `http://localhost:8042` | readsb `--net-api-port` (serves the list from `/`) |

Per-document path fallbacks inside each base:

| Document | Paths tried in order |
|---|---|
| aircraft | `/data/aircraft.json` → `/aircraft.json` → `/` |
| receiver | `/data/receiver.json` → `/receiver.json` |
| stats | `/data/stats.json` → `/stats.json` |

Exit codes: `0` success, `1` decoder error or aircraft not found, `2` usage
error. Positional arguments per command:

| Command | Arguments |
|---|---|
| `ls`, `watch`, `stats`, `squawks`, `emergency` | none |
| `info` | `HEX` (6 hex digits, optional `~` prefix) |
| `nearest` | `LAT LON` (−90…90, −180…180) |
| `box` | `LAT1 LAT2 LON1 LON2` (corner order irrelevant; `LON1 > LON2` wraps the antimeridian) |

## How it works

```
dump1090-cli
├── cmd/dump1090-cli      flag parsing, commands, JSON emitters, usage
└── internal
    ├── dump1090/         decoding + HTTP
    │   ├── types.go      tolerant aircraft.json/receiver.json/stats.json
    │   │                 types; each element keeps its raw bytes
    │   ├── client.go     path fallbacks, 32 MiB body cap, HTML rejection
    │   └── discover.go   base-URL resolution and probing
    ├── query/            pure functions: haversine/bearing, box and
    │                     nearest ranking, display sort, squawk grouping,
    │                     emergency detection
    ├── render/           table engine, colour/style decisions, the
    │                     aircraft/info/stats/squawks/emergency views
    └── term/             "is this a terminal" + ANSI capability (per OS)
```

Data flow for one invocation: `splitArgs`/`parseArgs` (flags allowed on
either side of the command) → `resolveArgs` (command + coordinate
validation, exit 2 on failure) → `dump1090.Resolve` picks the base URL →
`Client.FetchAircraft`/`FetchReceiver`/`FetchStats` → `query` filters and
ranks → `render` writes aligned columns with colour decided once by
`term`/`NO_COLOR`.

Design rules that keep it honest:

- **Tolerant decode, verbatim passthrough.** Every field except `hex` is
  optional, unknown keys are ignored — but the original bytes are kept in
  `Raw`, so `--json` never loses information the decoder sent.
- **Never trust the transport.** A response only counts as a document if it
  is HTTP 200, valid JSON of the expected shape (a reverse proxy's HTML
  page is rejected with `no usable response`), and under 32 MiB.
- **Distances are explicit.** Decoder `dst`/`dir` win; otherwise
  `receiver.json` is used; otherwise the row shows `-` instead of a made-up
  number.
- **Stdlib only.** `net/http`, `encoding/json`, `flag`, `regexp`,
  `sort` — nothing else, so `go build` produces a single static binary.

## Development

```sh
git clone https://github.com/technothatch/dump1090-cli.git
cd dump1090-cli
make lint     # gofmt -l, go vet, golangci-lint
make test     # go test -race -coverprofile coverage.out -covermode atomic ./...
make build    # go build ./...
make check    # everything CI runs
```

Without `make`, the same gates are:

```sh
gofmt -l .        # must print nothing
go vet ./...
go test ./...
go build ./...
```

Tests are table-driven and run against `httptest` servers that serve real
decoder documents — no network and no running decoder is required.

## Related projects

- [dump1090-fa](https://github.com/flightaware/dump1090) — FlightAware's
  Mode S decoder serving `/data/*.json`.
- [readsb](https://github.com/wiedehopf/readsb) — the decoder/network fork
  with the `--net-api-port` query API this CLI can also talk to.
- [tar1090](https://github.com/wiedehopf/tar1090) — the web map most people
  pair with either decoder.
- [graphs1090](https://github.com/wiedehopf/graphs1090) — long-term graphs
  for the same receiver.
- [dump1090-exporter](https://github.com/technothatch/dump1090-exporter) —
  Prometheus exporter for the same documents, by the same author.

## License

MIT — see [LICENSE](LICENSE).

---

## Related projects

Part of the [technothatch ADS-B toolkit](https://github.com/technothatch/technothatch#readme) - the full index with one-liners for every tool lives there.

- **Run & operate:** [dump1090-stack](https://github.com/technothatch/dump1090-stack) - full Compose stack; [dump1090-healthcheck](https://github.com/technothatch/dump1090-healthcheck) - decoder watchdog; [dump1090-alerts](https://github.com/technothatch/dump1090-alerts) - rule-based alerting
- **Metrics & data:** [dump1090-exporter](https://github.com/technothatch/dump1090-exporter) - Prometheus; [dump1090-influx](https://github.com/technothatch/dump1090-influx) - InfluxDB/Telegraf; [dump1090-grafana](https://github.com/technothatch/dump1090-grafana) - dashboards; [dump1090-recorder](https://github.com/technothatch/dump1090-recorder) - SQLite history; [dump1090-coverage](https://github.com/technothatch/dump1090-coverage) - heatmaps & reports
- **Feeds & networking:** [beast-relay](https://github.com/technothatch/beast-relay) - Beast fan-out; [dump1090-geofence](https://github.com/technothatch/dump1090-geofence) - region-filtered feeds; [dump1090-to-mqtt](https://github.com/technothatch/dump1090-to-mqtt) - MQTT/Home Assistant
- **Interfaces:** [dump1090-dashboard](https://github.com/technothatch/dump1090-dashboard) - web UI; [adsb-tui](https://github.com/technothatch/adsb-tui) - terminal radar; [dump1090-cli](https://github.com/technothatch/dump1090-cli) - aircraft.json CLI; [dump1090-sdk](https://github.com/technothatch/dump1090-sdk) - TypeScript client
- **Testing & decoding:** [dump1090-simulator](https://github.com/technothatch/dump1090-simulator) - synthetic traffic; [dump1090-replay](https://github.com/technothatch/dump1090-replay) - recorded history replay; [squawk-decoder](https://github.com/technothatch/squawk-decoder) - squawk/emitter/ICAO decoder
