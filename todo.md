# DarkStream — TODO

## Current Status
- [x] All four modules (`common`, `cli`, `worker`, `master`) build cleanly on Go 1.25.0.
- [x] End-to-end pipeline verified locally on 2026-08-12: scan → queue → dispatch → download → ffmpeg convert → upload → complete.
- [ ] **The Go test suite does NOT fully pass** — 4 failures in the master module. See "Verified findings" #2. (The previous claim that the suite passed as of 2026-04-13 only holds when the host TZ is UTC.)

---

## Verified findings from a local all-in-one run (2026-08-12)

These were reproduced by actually running master + worker on one host, not by reading code.
Ordered by severity.

### 1. [x] Every upload failed: multipart body truncated by defer ordering — **FIXED**
`video-converter-worker/internal/client/master_client.go:927`

Go runs `defer` LIFO. `pipeWriter.Close()` was registered *after* `multipartWriter.Close()`, so the
pipe closed **first** and the multipart closing boundary was never written. The master rejected every
upload with `400 Failed to parse multipart form`, and the worker logged the tell-tale
`Failed to close multipart writer: io: read/write on closed pipe`.

Effect: **100% of conversions failed at the upload step** — the whole product was non-functional
end-to-end. Conversion itself succeeded, which made it look like a master-side parsing bug.

Fix applied: swapped the two `defer` registrations so the multipart writer closes before the pipe.
After the fix all jobs complete and checksums match. This is the only source change made.

- [ ] Add a regression test that asserts a real multipart upload round-trips (this path had no test).
- [ ] Surface a clearer error: a server-side rejection currently surfaces as `io: read/write on closed pipe`
      from the copy goroutine rather than the actual HTTP status, which sent debugging down the wrong path.

### 2. [ ] Timezone bug: stale-job detection and worker liveness are wrong off UTC — **HIGH**
`video-converter-master/internal/db/tracker.go:1005` (and sibling queries)

Timestamps are written with Go's `time.Now()` (host-local) but compared against SQLite's
`datetime('now')`, which is **always UTC**. The two are offset by the host's UTC offset.

Reproduced: these 4 tests fail at UTC+02:00 and pass under `TZ=UTC`:
`TestGetActiveWorkers`, `TestGetJobHistory`, `TestGetStaleProcessingJobs`, `TestHealthCheck_StaleJobs`.

This is not just a test bug — in production:
- **East of UTC** (e.g. CEST): stale jobs are detected `offset` hours *late*, so crashed-worker jobs
  stay stuck in `processing` for hours; dead workers keep counting as active.
- **West of UTC** (e.g. US): jobs are declared stale `offset` hours *early*, so **healthy in-flight
  jobs get killed and requeued**, wasting work and potentially looping.

- [ ] Store all timestamps in UTC (`time.Now().UTC()`) and/or use `strftime('%Y-%m-%d %H:%M:%S','now')`
      consistently; never mix Go-generated and SQLite-generated time in one column.
- [ ] Add a CI job that runs the suite under a non-UTC `TZ` to keep this from regressing.

### 3. [x] Hardcoded 1 MB minimum output size failed all short/small videos — **FIXED**
`video-converter-worker/internal/converter/ffmpeg.go`

`MinOutputFileSize = 1 MB` rejected any smaller output as invalid, permanently failing the job. A valid
5-second 640x480 clip converted to 891 KB and was failed with `output file too small`. Short clips,
trailers and low-bitrate content are all legitimately below any size threshold worth setting.

Replaced with a real validity check via ffprobe: the output must be readable, contain a video stream,
have a playable duration, and — when the source length is known — be about as long as the source
(2s or 2% tolerance, whichever is larger). The size floor is now 1 KB, purely to catch empty or
truncated files before spending an ffprobe call.

This is strictly stronger than the old check: a duration comparison catches a conversion that stopped
early *regardless of file size*, which is the failure the size threshold was actually aiming at, while
64 KB of garbage with no video stream is now rejected where a size-only check would have passed it.
Covered by 7 tests.

### 4. [ ] Rate limiter starves the control plane — **HIGH**
`video-converter-master/internal/server/http.go:2441`, `126`

`rateLimitMiddleware` applies a **hardcoded 100 req/min per IP** across *all* endpoints, sharing one
token bucket between high-frequency progress updates and critical control-plane calls.

Observed: progress updates burned the bucket, causing `429` on `upload-video` and `next-job`. One
upload failed its first attempt purely from throttling, and even `darkstream-cli status` was rejected
with `429` while the worker was busy. The failure surfaced as a confusing closed-pipe error (see #1).

- [ ] Separate buckets per endpoint class (progress/telemetry vs. job control vs. transfers), or exempt
      transfers and job dispatch entirely.
- [ ] Honour the configured `max_api_requests_per_min` — the server ignores it and hardcodes 100/min,
      so the master/worker config keys are misleading.
- [ ] Key the limiter on worker identity, not IP: every worker on one host (or behind NAT) shares a
      single bucket today, so the limit tightens as workers are added — the opposite of what's wanted.
- [ ] Throttle progress reporting client-side (the worker emitted many updates per second per job).

### 5. [ ] Aspect ratio is distorted — portrait sources are squashed to landscape — **MEDIUM**
`video-converter-worker/internal/converter/ffmpeg.go:211`

The filter is built as `-vf scale=<target_resolution>` with no aspect handling. Both 720x1280
portrait test videos were emitted as 1280x720 landscape — geometrically distorted, not letterboxed.

- [ ] Use `scale=W:H:force_original_aspect_ratio=decrease` (+ `pad` if fixed output dimensions are
      required), and consider never upscaling sources smaller than the target.

### 6. [ ] `master.yaml` conversion settings are a first-run seed only — silent config override — **MEDIUM**
`video-converter-master/internal/config/manager.go:73`, `run/db/active-config.json`

The authoritative conversion config lives in a persisted `active-config.json` next to the DB. The YAML
`conversion:` / `worker_defaults:` blocks only seed it on first run. After that, editing the YAML has
**no effect** and the file silently disagrees with actual behaviour.

Observed live: the active config changed to `h265 / 8M / 1920x1080` while the YAML said
`h264 / 2M / 1280x720`, and a requeued job re-encoded to 13.8 MB instead of 3.5 MB from the same source.

- [ ] Document the precedence clearly in README/DOCKER.md and in the YAML comments.
- [ ] Log loudly at startup when persisted config differs from the YAML.
- [ ] Provide a supported way to force a re-seed from YAML (flag or CLI command).
- [x] **Resolved (2026-08-12):** the "unexplained" config version bump was almost certainly the
      operator using the dashboard's Configuration form. The values involved (`h265`, `8M`,
      `medium`, and later `hwaccel: vulkan`) appear nowhere in the code, defaults, or any config
      file, the worker only ever issues `GET /api/config`, and the integration tests bind their own
      ports (18080/28080/38080). Not a bug — but see the usability problem it exposes below.

### 7. [ ] Scanner re-hashes the entire library on every scan — **MEDIUM**
Every scan interval (1 min in this run) the master recomputed SHA-256 for **every** file to detect
duplicates, logging `Duplicate file detected` for each on every pass. On a real media library this is
continuous full-library disk I/O and will not scale.

- [ ] Skip hashing when path + size + mtime are unchanged; cache hashes in the DB keyed on those.

### 8. [ ] Inconsistent auth enforcement — **LOW (consistency, not a hole)**
Most routes wrap `authMiddleware`, but `/api/job/cancel`, `/api/jobs/cancel`, `/api/config` and
`/api/stats` do not — they call `requireAPIKeyAuth` inline instead. Verified as genuinely enforced, so
this is **not** a vulnerability, but the split convention makes it easy to add an unprotected route by
accident and hard to audit.

- [ ] Standardise on middleware-based auth so route registration alone shows what is protected.

---

## Project direction: Vulkan + CPU only, on FFmpeg 9 (2026-08-12)

### 18. [x] Upgraded to FFmpeg 9 and dropped NVENC — **DONE**

**Direction:** DarkStream accelerates with **Vulkan** (open, vendor-neutral) or plain **CPU**
encoding. Vendor-specific backends — NVENC, and by extension QSV/VAAPI/CUDA — are deliberately not
supported.

**FFmpeg 9 is only viable because of that decision.** Measured on this host (Tesla P4, driver
580.142):

| | FFmpeg 7.1.5 (Debian) | FFmpeg 8.1.2 | FFmpeg 9 (9.1-dev) |
|---|---|---|---|
| CPU (libx264) | works | works | works |
| Vulkan | **fails** | works | works |
| NVENC | works | works | **fails** — needs driver 610+ |

FFmpeg 9's NVENC requires NVIDIA driver 610.00+ (`Required: 13.1 Found: 13.0`); this host has
580.142. That would have been a blocking regression, but since NVENC is being dropped anyway it costs
nothing — Vulkan and CPU, the two backends we keep, both work on 9.

Installed at `/opt/ffmpeg9/bin/ffmpeg`; `run/worker.yaml` points at it. FFmpeg 8.1.2 remains at
`/opt/ffmpeg8` as a fallback (it is the only build here where *all three* backends work, if NVENC is
ever needed again).

**Build:** compiled from source off the upstream **`release/9.0`** branch (currently **9.0.1**), which
carries the stable release plus backported bug fixes. No static-build provider ships a stable 9.0.x —
BtbN publishes only the 7.1/8.1 release branches plus `master`, and johnvansickle's "release" build is
still 7.0.2 from 2024 — so a `master`/9.1-dev snapshot was used briefly before being replaced by this
source build.

Configured deliberately narrow for what DarkStream actually uses:

```
./configure --prefix=/opt/ffmpeg9 --enable-gpl --enable-version3 \
            --enable-libx264 --enable-vulkan --disable-doc --disable-debug
```

`--enable-libx264` covers CPU encoding, `--enable-vulkan` the GPU path. NVIDIA's codec SDK is absent
from the build entirely, so the binary cannot use NVENC even by accident — the Vulkan-only policy is
enforced by the toolchain, not just by configuration.
- [ ] Rebuild periodically to pick up `release/9.0` backports (it is a branch, not a fixed tag).

**Code changes:** NVENC removed from the encoder selection, preset mapping, capability probe, config
validation, aggregate capability endpoint and both Web UI dropdowns. The remaining backends are
`none` and `vulkan`.

**Migration for existing installs:** a stored `hwaccel: nvenc` (or `cuda`/`qsv`/`vaapi`) is rewritten
to `none` with a warning, for both the global setting and per-library overrides. This runs **before**
validation deliberately — a retired value would otherwise fail validation on load and silently reset
the entire configuration, including every configured library, back to defaults. Covered by a
regression test that asserts the libraries survive.

Verified end-to-end on the **9.0.1 source build**: the worker reports `ffmpeg version n9.0.1`, the
capability probe returns `[none vulkan]`, and a 60s 1920x1080 job converted to HEVC via Vulkan at
**49% GPU encoder utilisation**, output verified valid.

- [ ] Ship FFmpeg 9 in the Docker image — Debian 13's 7.1.5 cannot do Vulkan encode at all.
- [ ] Document the FFmpeg 8+ (ideally 9) requirement for Vulkan in the README.

---

## Reported issues, investigated 2026-08-12 (round 2)

### 9. [x] Web UI never auto-updated: SSE listener bound to the wrong event — **FIXED**
`video-converter-master/internal/server/webui.go:819` (client), `http.go:897` (server)

The server emits a **named** SSE event (`event: stats\ndata: {...}`), but the dashboard bound its
handler with `evtSource.onmessage`. `onmessage` only fires for *unnamed* (default `message`) events,
so the handler **never ran once**. The connection opened fine and the "Live" indicator went green,
which is why it looked like a data problem rather than a wiring problem.

Confirmed against the running server — every frame on the wire begins `event: stats`.

Everything the UI needs was already in the payload (`stats`, `workers`, `pending_jobs`,
`processing_jobs`, and `recent_jobs` carrying completed + failed rows), so no server change was
needed. Fixed by binding `evtSource.addEventListener('stats', ...)` instead.

- [ ] Add a smoke test that asserts the dashboard subscribes to the event name the server emits —
      these two are edited in different files and drifted apart silently.

### 10. [x] Worker ignored Web UI config changes — **FIXED**
`video-converter-worker/internal/client/config.go:233`, `internal/worker/worker.go:581`

Two independent causes:

1. **The acceleration flag was never remote at all.** The job path hardcoded
   `UseVulkan: w.config.FFmpeg.UseVulkan` — read from the worker's *local* `worker.yaml`. Toggling it
   in the Web UI could not affect a worker under any circumstances, even after a restart.
2. **The worker's config parser silently dropped unknown fields.** It decodes the master's
   `ActiveConfig` into a private inline struct listing only resolution/codec/bitrate/preset. Any field
   not in that list is discarded with no error. This bit the new `hwaccel` field immediately — the
   master served `hwaccel=nvenc` and the worker logged `hwaccel=""`.

Fixed both: acceleration now comes from the master's live config, and the parser carries the field
through. Verified live — changing the backend in the Web UI applied to the next job **without
restarting the worker** (settings refresh on a 30s TTL).

Also added `refreshRemoteWorkerConfig`: the worker now re-reads the master's *worker* settings every
30s. Previously these were fetched once at startup (`main.go:89`) and only in `-url` mode, so they
never changed on a running worker. Bandwidth limit is hot-applied; settings that are genuinely bound
at startup (concurrency, poll intervals) now log a "restart required" warning rather than appearing
to work. `MasterClient.bandwidthLimit` was made an `atomic.Int64` first — hot-applying it as a plain
field would have raced with in-flight transfers. `go test -race` is clean.

- [ ] Make the worker's config decoding share the master's `ActiveConfig` type (or error on unknown
      fields) so the next added setting cannot be silently dropped the same way.
- [ ] Hot-apply concurrency by resizing the worker pool.

### 11. [~] Vulkan acceleration — **root-caused; NVENC implemented as the working path**

Three separate problems, and the honest headline is that **Vulkan encode cannot currently work on this
machine, but the GPU can**. Detail, because the conclusion contradicts the original assumption:

**a. [x] The binary was never built with Vulkan support.** Real detection sits behind `//go:build
vulkan`; the default build links a stub that hardcodes "not supported" — hence
`Vulkan support not compiled in, using stub implementation`. Built with `-tags vulkan`, detection
works perfectly and finds the card: `Tesla P4, discrete, driver 580.142, api 1.4.312, can_encode=true,
max 7680x4320`. The tag needs `libvulkan-dev libwayland-dev libx11-dev libxcb1-dev libxrandr-dev` at
build time. Exposed as opt-in: `VULKAN=1 ./run/all-in-one.sh start`.
- [ ] Add the same tag + dev packages to the `Dockerfile` (it builds untagged today, so every
      container ships the stub).
- **[x] Upstream fixed (2026-08-12), upgraded to v1.2.2.** The windowing-header requirement was an
  upstream defect, reported as
  [darkace1998/Golang-Vulkan-api#121](https://github.com/darkace1998/Golang-Vulkan-api/issues/121)
  (now closed). `surface_linux.go` was split into `surface_linux_xlib.go` / `surface_linux_wayland.go`
  behind `vk_headless` (and per-backend `vk_no_xlib` / `vk_no_wayland`) tags, and the docs were
  corrected to state that default builds need X11/Wayland headers while headless builds need only
  `libvulkan-dev`.

  DarkStream now builds with `-tags vulkan,vk_headless` (`VULKAN=1 ./run/all-in-one.sh start`), which
  drops the X11/Wayland build dependency entirely — a transcoding server has no windows. Verified:
  `go list -tags vk_headless` compiles no surface files, the worker builds and still detects the
  Tesla P4 correctly, and the capability probe is unchanged.
  - [ ] The Dockerfile can now enable Vulkan detection with only `libvulkan-dev` + `pkg-config`,
        without pulling a display stack into the image.

  v1.2.2 also carries real video-encode fixes (spec-conformant encode capability chaining, encode
  session profile propagation, memory bind indices). None of them change our situation: the library
  still reports `can_encode=true` for the Tesla P4 because that comes from extension presence, while
  the actual failure is in **FFmpeg's** `h264_vulkan` encoder. The capability probe (#17) remains the
  thing that catches it — which is exactly why it probes with a real encode rather than trusting a
  capability query.

**b. [x] RESOLVED (2026-08-12): Vulkan encoding now works — it needed FFmpeg 8.x *and* a fix to our
own command construction.** Two independent causes, both now fixed:

1. **FFmpeg 7.1.5 could not do it.** Debian 13 ships 7.1.5 and caps there. A static FFmpeg **8.1.2**
   build encodes `h264_vulkan` and `hevc_vulkan` on this exact Tesla P4 / driver 580.142 without
   complaint. Installed to `/opt/ffmpeg8/bin/ffmpeg`; point workers at it with `ffmpeg.path`.
2. **Our Vulkan command was wrong.** `buildFFmpegCommand` emitted `-hwaccel vulkan -hwaccel_device 0`,
   which configures *decoding* but never gives the filter graph a device — so the `hwupload` needed to
   feed the Vulkan encoder failed with `A hardware device reference is required to upload frames to`.
   This was masked for as long as FFmpeg 7 failed earlier in the pipeline; upgrading surfaced it as a
   different error. Fixed by initialising a named device instead
   (`-init_hw_device vulkan=vk:0 -filter_hw_device vk`) and keeping decode in software so the scale
   filter still works on ordinary frames, with only the final upload crossing to the GPU.

Verified end-to-end: the capability probe now reports `working=[none nvenc vulkan]`, and a real job
converted to **HEVC 1920x1080 via Vulkan at 50% GPU encoder utilisation**.

The capability probe proved its worth twice here — it caught the original breakage, then confirmed the
fix without anyone having to reason about driver support.

- [ ] Ship FFmpeg 8.x in the Docker image (Debian 13 has only 7.1.5, which cannot do Vulkan encode).
- [ ] Document the FFmpeg 8+ requirement for Vulkan in the README.

**b-history. Original finding (FFmpeg 7.1.5): Vulkan encoding failed — not a DarkStream bug.**
The P4 advertises everything needed (`VK_KHR_video_encode_queue`, `..._h264`, `..._h265`, and a
dedicated encode queue family), and ffmpeg selects the device happily. But the encoder fails to open:
`Error while opening encoder - maybe incorrect parameters`, exit 218 — reproduced both standalone and
through a real DarkStream job. Same result for `h264_vulkan` and `hevc_vulkan`, with and without a
correct `hwupload` filter chain. This is ffmpeg 7.1.5 / driver 580.142, where Vulkan video encode is
still very new.
- Note the detector reports `can_encode=true` purely from extension presence, so it **over-promises**:
  it will happily enable a backend that then fails every job.
- [ ] Have detection probe an actual 1-frame encode before declaring a backend usable.
- [ ] Re-test Vulkan encode after an ffmpeg/driver upgrade.

Two real bugs were fixed in this path regardless: the Vulkan filter chain never uploaded frames to the
device (`scale` output went straight to a hw encoder — guaranteed format failure), and NVENC would
have been handed libx264 preset names, which it rejects outright (`Undefined constant 'ultrafast'`).

**c. [x] NVENC added, and the GPU now genuinely does the work.** NVENC works flawlessly on this P4.
Added an `hwaccel` setting (`none` / `nvenc` / `vulkan`) to the conversion config, the `/api/config`
API, and the Web UI's Video Settings card, with `-hwaccel cuda` decode, correct encoder selection
(`h264_nvenc` / `hevc_nvenc`) and preset mapping (`ultrafast`→`p1` … `veryslow`→`p7`).

Verified end-to-end: 90s of 1080p transcoded to 720p with **NVENC encoder utilisation at 42-43% and
182 MiB of GPU memory in use**, valid output, job completed. Legacy `use_vulkan` still honoured when
`hwaccel` is unset.

- [ ] Consider `hwaccel: auto` that probes NVENC → Vulkan → CPU and picks the first that really encodes.

---

## Feature work, 2026-08-12 (round 3): multi-library + web-first configuration

### 12. [x] Multiple video libraries, configurable in the Web UI — **DONE**

DarkStream previously scanned exactly one folder (`scanner.root_path`). It now supports any number
of independent libraries, all feeding the same job queue.

A library has a name, an absolute source path, its own output path, an enabled flag, optional
per-library file extensions and recursion depth, and an optional "replace source" mode that converts
in place. Manage them under the new **📚 Libraries** tab, or via `/api/libraries`
(`GET` list, `POST` create/update, `DELETE ?id=`).

Implementation notes:
- One `scanner.Scanner` per enabled library, rebuilt whenever the configuration changes — **adding a
  library takes effect immediately, with no master restart**. Each library also gets its own fsnotify
  watcher.
- Jobs carry a new `library_id` (new `jobs.library_id` column, added by the existing migration path),
  so jobs can be attributed and counted per library. Pre-existing jobs keep an empty library ID.
- The server's path allow-list is now derived from the configured libraries and updated live. Without
  this, a newly added library's files would be rejected with `403 Invalid file path`.
- A failing library (unmounted share, bad path) is logged and skipped rather than aborting the scan
  for every other library.
- Validation rejects relative paths, missing names, duplicate IDs, and two libraries pointing at the
  same tree (which would queue every file twice).

Verified end-to-end: two libraries added at runtime, each scanned immediately, each job routed to its
own output directory, converted via NVENC.

### 13. [x] Configuration moved off `config.yaml` to the web store — **DONE**

The master now starts with **no YAML file at all**. Verified: launched with `-config /nonexistent.yaml`
and it came up clean, reporting `libraries=0` and "add one in the Web UI to start converting".

- `active-config.json` (next to the database) is the authoritative store and now holds libraries,
  scanner settings and notification settings in addition to video/audio/output/worker settings.
- The YAML file is **only** a first-run seed. When present, a legacy `scanner.root_path` is migrated
  into a single library named "Default", so existing deployments keep working after upgrading.
- Config files written by older versions are backfilled with the new sections on load rather than
  being reset.
- Bootstrap settings that cannot live in the web store (you need them to reach the Web UI) come from
  environment variables: `DARKSTREAM_HOST`, `DARKSTREAM_PORT`, `DARKSTREAM_DB_PATH`,
  `DARKSTREAM_CONFIG_PATH`, `DARKSTREAM_API_KEY`, `DARKSTREAM_TLS_CERT`, `DARKSTREAM_TLS_KEY`,
  `DARKSTREAM_LOG_LEVEL`, `DARKSTREAM_LOG_FORMAT`. Everything else is managed in the browser.
- Try it with `NO_YAML=1 ./run/all-in-one.sh start`.

Two bugs were found and fixed while building this:

- **`POST /api/config` silently deleted whole config sections.** It replaced the entire stored config
  with the request body, so any client that omitted `libraries` (including the dashboard's own
  Configuration form) wiped every configured library. Caught live — the path allow-list went 2 → 4 →
  **0** and all jobs began failing with `403 Invalid file path`. The handler now decodes onto a copy
  of the current config, so omitted sections are preserved and only supplied keys change. This is the
  same "silently dropped field" failure mode as finding #10.
- **Fresh installs defaulted to `use_vulkan: true`.** With no YAML, a brand-new install enabled a
  hardware backend that (per finding #11b) cannot encode on this host, so *every* job failed with
  ffmpeg exit 234. Hardware acceleration is now opt-in: the default is `hwaccel: none` / CPU, which
  works everywhere. Users turn on NVENC deliberately.

- [ ] Move the remaining scanner/notification settings into dedicated Web UI cards (the API and store
      support them; only the Libraries tab has UI so far).
- [ ] Consider per-library conversion profiles (different resolution/codec per library) — the config
      model has room for it, but jobs currently share one global conversion setting.
- [ ] Deprecate `config.yaml.example` in the docs once the remaining UI cards land.

### 14. [x] History view shows the library instead of the full path — **DONE**

The History table rendered raw absolute source paths, which are long, repetitive and truncate badly.
It now has a **Library** column (badge) and a **File** column showing the path *relative to that
library*, e.g. `Movies` + `action/2024/nested_film.mp4`. The full path is still there as the cell's
tooltip, and job links are unchanged.

- A shared `JobView` decorates jobs with `library_name` / `rel_path`, used by both the server-rendered
  table and the SSE payload, so live updates and full page loads agree.
- Jobs created before the `library_id` column exists are attributed by **path prefix**, so upgrading
  does not turn the whole history into "Unknown". A stale `library_id` (library since deleted) falls
  back the same way.
- Prefix matching picks the longest match, so a library nested inside another wins, and a sibling
  directory sharing a name prefix (`/mnt/movies-archive` vs `/mnt/movies`) does not match.
- Anything that still cannot be attributed shows `Unknown` with the bare filename.

Verified live across five libraries including a nested file and legacy jobs.

- [ ] Apply the same treatment to the Pending and Processing tables (they still show full paths).
- [ ] Consider filtering history by library now that the column exists.

### 16. [x] Per-library conversion settings — **DONE**

Each library can now override any conversion setting: resolution, video codec, bitrate, preset,
output format, audio codec, audio bitrate, and hardware acceleration. Every field is optional — blank
means "inherit the global setting", so a library can override only its resolution and inherit the
rest. Edit them in the Libraries tab (a **Conversion Settings** section on the library editor); the
table's new **Settings** column shows each library's effective profile and badges the ones with
overrides.

How the settings reach the worker:
- The master resolves the effective settings (global + that library's overrides) **at dispatch time**
  and attaches them to the job as a transient `conversion` field. It is never stored in the database.
- The worker prefers the job's settings and only falls back to its periodically fetched global config
  when they are absent, so a mixed fleet and older jobs keep working.
- Resolving per job rather than per batch matters: `/api/worker/next-jobs` can hand a worker jobs from
  several libraries at once, each needing a different profile.
- Overrides are validated with the same rules as the global settings, so a bad value is rejected when
  the library is saved instead of failing every job in that library later.

Verified end-to-end with two libraries converting the same source material differently:
`LibA → clip_a.mp4 (h264, 1280x720)` and `LibB → clip_b.mkv (hevc, 854x480)`, plus a third library
with no overrides correctly inheriting the global `1920x1080 / h264 / 5M / mp4`.

**Pre-existing bug fixed along the way:** the scanner hardcoded `.mp4` for every output path
(`scanner.go:221`) regardless of the configured output format, so setting format to mkv/webm/avi
produced a file with the wrong extension — the worker converted to the right container but uploaded
it to a `.mp4` destination. Output paths now follow the format that applies to the library. Note that
this affects *newly discovered* files; jobs already in the queue keep the path they were created with.

- [ ] Allow overriding scan settings (min/max size, hidden files) per library too — currently only
      extensions and recursion depth are per-library.
- [ ] Show the effective profile on the job detail page.

### 17. [x] Workers auto-detect their real encoding capabilities — **DONE** (closes #15, replaces #11b detection)

Workers now determine what they can actually do instead of trusting what the driver claims.

**How the probe works.** At startup a worker renders a tiny clip, then runs a real conversion for each
backend (`none`, `nvenc`, `vulkan`) **through the same `buildFFmpegCommand` path production jobs
use**. That fidelity is the point: probing with a hand-written command could pass while real jobs fail,
because the hwaccel flags, filter chain and preset mapping are exactly where these backends break.
Each backend is reported as *not built in* / *unusable* / *working*, and the specific FFmpeg error is
kept when it fails (FFmpeg's generic `Conversion failed!` closing lines are skipped in favour of the
real reason).

Results are sent with every heartbeat, stored as JSON on the worker row (new `workers.capabilities`
column, added by migration), and exposed at `/api/workers` per worker and `/api/capabilities`
aggregated across online workers.

**In the UI:** a *Detected Encoding Capabilities* table in the Workers tab shows every worker's
backends with the failure reason, and both hardware-acceleration dropdowns (global Configuration and
per-library) now label options no worker can run as *"not supported by any worker"*, with the reason
in the tooltip. That closes finding #15 — picking a backend that silently fails every job.

**This immediately paid for itself on this host.** The probe reports exactly what the earlier
extension-based detection got wrong:

| Backend | Available | Working |
|---|---|---|
| none (libx264) | yes | **yes** |
| nvenc (h264_nvenc) | yes | **yes** |
| vulkan (h264_vulkan) | yes | **no** — `Error sending frames to consumers: Invalid argument` |

The old detector reported `can_encode=true` for Vulkan because the Tesla P4 advertises the encode
extensions. It cannot actually encode, and every Vulkan job on this host failed as a result. GPU
identification also flows from the same probe, so the reported GPU and the verified encoders always
agree (`Tesla P4` when built with `-tags vulkan`).

- [ ] Use capabilities in scheduling: skip assigning a job to a worker whose required backend is
      unusable, instead of letting it fail. Needs per-worker filtering in the claim query.
- [ ] Re-probe periodically (or on demand) so a driver upgrade is picked up without a worker restart.
- [ ] Probe per codec (h264 vs h265) rather than assuming h264 support implies the rest.

### 21. [x] Per-job logs in the job history — **DONE**

A failed job previously showed a single line (`ffmpeg conversion failed: exit status 234`), with the
actual cause discarded — FFmpeg's stderr was thrown away (`io.Copy(io.Discard, stderr)`). Diagnosing
anything meant reproducing it by hand, which is what most of this session's debugging consisted of.

Workers now record a log per job and report it to the master. Every row in the **History** tab has a
**Log** button that expands the log inline; the job detail page also shows it as a **Job Log** card.

What gets captured:
- stage transitions (download → convert → upload → complete), with durations,
- the resolved conversion settings for that job's library,
- **the exact FFmpeg command line**, and
- **FFmpeg's own output**, which was previously discarded.

Design points:
- Progress lines (`frame=`, `fps=`, `out_time=` …) are filtered out — they are high-frequency machine
  output that would drown the log.
- Severity is inferred per line (`Could not open encoder` → error, `deprecated` → warning) so the real
  failure stands out in red instead of being buried in uniform text.
- Capped at 300 entries, keeping the **first and last** lines and inserting an
  `… N earlier lines omitted …` marker. The start explains what was attempted and the end holds the
  failure; dropping only the middle preserves both.
- The collector is mutex-guarded because FFmpeg output is drained on its own goroutine while the job
  goroutine records stages (`go test -race` clean).
- Logs are replaced (not appended) per run, so a requeued job shows the run that produced its current
  state, and they are deleted when jobs are pruned so the table cannot grow unbounded.
- Sent once at the end of a job via `POST /api/worker/job-log`, not streamed — the rate limiter is
  already a known pressure point (#4). Failing to send a log never fails the job.

New: `job_logs` table, `POST /api/worker/job-log`, `GET /api/job/logs?job_id=`.

Verified on both outcomes: a successful Vulkan job (51 entries, ending `Job completed successfully in
3.361s`) and a deliberately broken one, where the log now shows the full chain —
`Error reinitializing filters!` → `Could not open encoder before EOF` → `Conversion failed!` — instead
of just the exit status.

- [ ] Stream logs live for in-progress jobs (currently they appear when the job finishes).
- [x] Log button directly in the History row — the first version only put the log on the job detail
      page, reachable by clicking the job ID, which was not discoverable.
- [x] **The log is now a modal overlay, not an inline table row.** Two bugs led here, both rooted in
      the SSE handler rebuilding the History table wholesale every 2s (`tbody.innerHTML = ''`):
      first the injected log row was destroyed about a second after opening; then, once the row was
      restored after each rebuild, re-creating those nodes under the cursor wiped any text selection
      and could leave the browser selecting unrelated content. Re-rendering a region the user is
      interacting with is the underlying mistake, so the log moved into a modal that lives outside
      the table and is never touched by refreshes. Selection, scroll position and content now
      persist, plus Escape/backdrop close and a Copy button.

      Regression-tested with jsdom against the real served page (`scratchpad/logtest.js`,
      16 assertions) including "text stays selected across a table rebuild". The earlier inline
      version's test also caught a button-label bug, so the harness has paid for itself twice.
- [ ] Include download/upload transfer detail in the log, which currently only records start/finish.

### 19. [x] Files scanned mid-write get a checksum that can never match — **FIXED**

A file still being written was picked up and hashed, recording the checksum of a partial file. When a
worker later downloaded the finished file the checksums differed and the job failed with
`checksum mismatch: downloaded file checksum does not match expected value`. Requeueing could not
help — the stale checksum was stored on the job, so every retry failed identically and the job was
permanently unrunnable. This hit **every file** copied into a watched folder.

Two independent causes, both fixed:

**1. Nothing stopped a mid-write file from being scanned.** The watcher debounced 2s after the last
event, but the *periodic scanner* had no protection at all and would hash whatever it found.
- The periodic scanner now skips files modified within `scanner.min_file_age` (default 10s), so a
  file still being copied is left alone until the writer stops.
- The watcher instead confirms stability directly: it stats the file, waits briefly, and stats again,
  leaving it for a later event if size or mtime changed. Using the age rule there would have delayed
  every watched file by 10s for no benefit, since the debounce already establishes quiet.

**2. A mismatch was permanent.** The master now treats it as recoverable: on a reported mismatch it
recomputes the source checksum, and **only if the file has genuinely changed since scanning** does it
refresh the stored checksum and requeue. If the file still hashes to the stored value the copy was
corrupted in transfer, which is a real failure and is reported as before. Recovery is bounded by the
job's normal retry budget so it cannot loop.

**3. The first version of the recovery refused to help the jobs that needed it most.** It was bounded
by the job's retry budget, but jobs poisoned *before* the fix had already spent their retries, so
recovery bailed out with `Not refreshing checksum: job has exhausted its retries` — the reported
symptom did not change. That guard was wrong in principle too: correcting a stale checksum repairs
faulty *metadata*, and the earlier failures were caused by that metadata rather than by the job. The
retry budget is no longer consulted; instead the refresh only happens once the file has stopped
changing (which is what actually bounds it), and the retry counter is reset when the checksum is
corrected. Requeueing also refreshes a stale checksum, so a stuck job can be rescued directly from
the UI.

Verified against the real failures on the running instance: three jobs stuck this way — including a
673 MB episode at `retry_count=3` — were healed by requeueing
(`Refreshed stale source checksum while requeueing`), and a re-scan confirmed **zero** jobs left with
a stale checksum.

Verified end-to-end: a 19 MB file written into a library in chunks over ~15s (simulating a real
upload) is now left alone until complete and converts successfully; and a job whose source was
swapped after scanning logs
`Source file changed after scanning; refreshed checksum and requeued job` and then completes.

### 20. [ ] `/api/job/requeue` returns 400 for jobs missing from `/api/jobs` — **LOW**

Requeueing a completed job that no longer appears in the default `/api/jobs` listing returned `400`.
The listing appears to be a subset (recent/active), so a valid job ID can look invalid to a caller
that discovered IDs from that endpoint. Minor, but confusing when scripting against the API.

- [ ] Make the jobs listing's scope explicit (or paginated), and have requeue distinguish
      "unknown job" from "job not in this view".

### 15. [x] The UI offers hardware backends that cannot work on the host — **RESOLVED by #17**

Selecting **Vulkan** in the Configuration form is accepted silently and then fails every subsequent
job (ffmpeg exit 234 on this host, see #11b). The dropdown offers all three backends unconditionally
with no capability check and no feedback.

This is the practical consequence of the over-promising detection noted in #11b, and it is easy to
walk into: the setting looks like it applied, and the only symptom is that all conversions start
failing.

- [ ] Probe each backend with a one-frame encode and disable (or badge) the ones that fail.
- [ ] Surface the connected workers' detected capabilities in the form.
- [ ] Warn when a selected backend has never successfully completed a job.

### 22. [x] Vulkan deadlocked on real HEVC sources — jobs hung forever with no GPU use — **FIXED**

Reported as "processing is so slow, no GPU utilisation, just a process using it". It was not slow — it
was **deadlocked**. A 673 MB HEVC episode sat with its output file frozen at 47 MB, ffmpeg at 3.9%
CPU and 0% GPU, every thread parked in `futex_do_wait` (including the Vulkan driver threads
`[vkcf]`/`[vkrt]`/`[vkps]`), and the process ignoring SIGTERM.

Isolated by bisection, each step run standalone:

| Test | Result |
|---|---|
| Synthetic h264 source → Vulkan | works |
| Real HEVC source → Vulkan | **hangs** (even 3 seconds of it) |
| Same content re-encoded to h264 → Vulkan | works |
| Real HEVC source → libx264 (CPU) | works, 257 fps / 10.7x |
| Real HEVC source → Vulkan, `-threads 1` | works |
| Real HEVC source → Vulkan, `-thread_type slice` | **works, 104 fps / 4.3x** |

The trigger is **frame-threaded software HEVC decoding feeding the Vulkan upload path**. Fixed by
adding `-thread_type slice` to the Vulkan branch, which keeps decoding multithreaded (single-threaded
decode also avoids the deadlock but is far slower).

Verified end-to-end: the same 673 MB episode that previously hung forever now completes in **296s**
(~24 minutes of video, ≈4.9x realtime) with the GPU encoder active throughout.

**Not a `golang-vulkan-api` problem.** The deadlock reproduces with a bare `ffmpeg` command line and
no Go code running at all, against a C binary that has no connection to the library. That library is
used only by `vulkan_detector.go` for device enumeration at worker startup and is never called during
a conversion — ffmpeg talks to Vulkan through its own libavutil code. This is an ffmpeg 9.0.1 /
driver 580.142 issue.

- [ ] Report the deadlock upstream to FFmpeg with the bisection above.
- [x] **libx265 added to the build** (`--enable-libx265`, x265 4.1). An h265 job on the CPU backend
      previously failed with `Encoder not found`; it now works, verified producing valid HEVC.
      Vulkan remains far faster for h265 — **4.3x realtime vs 0.47x** on this hardware, i.e. ~5 min
      versus ~51 min for a 24-minute episode — so CPU h265 is a fallback, not a replacement.
- [x] **Every codec the Web UI offers is now in the build.** Added `--enable-libvpx --enable-libaom
      --enable-libmp3lame --enable-libopus --enable-libvorbis` alongside libx264/libx265, so no
      dropdown option can fail with `Encoder not found` any more. Each was verified by performing a
      real encode and probing the result, not merely by checking that the encoder was listed:

      | UI option | Encoder | Verified |
      |---|---|---|
      | h264 | libx264 | encodes, probes as h264 |
      | h265 | libx265 | encodes, probes as hevc |
      | vp9 | libvpx-vp9 | encodes, probes as vp9 |
      | av1 | libaom-av1 | encodes, probes as av1 |
      | aac | aac | encodes, probes as aac |
      | mp3 | libmp3lame | encodes, probes as mp3 |
      | opus | libopus | encodes, probes as opus |
      | vorbis | libvorbis | encodes, probes as vorbis |

      All four output formats mux correctly too (mp4, mkv/matroska, webm, avi), and Vulkan encoding
      still works after the rebuild — checked for regression rather than assumed.
- [ ] Have the capability probe test per codec rather than assuming h264 support implies the rest —
      it reported the CPU backend as working while h265-on-CPU could not run at all.

---

## Notes for anyone reproducing this

- Run the stack with `./run/all-in-one.sh start` (build + master + worker on one host);
  `stop` / `status` / `logs` / `reset` also available. Configs in `run/master.yaml`, `run/worker.yaml`.
- Dashboard on `http://localhost:8080/`, Prometheus on `/metrics`, SSE on `/api/stats/stream`.
- Requires `ffmpeg` on PATH. Build with `VULKAN=1` for real GPU detection (see finding #11).
- **FFmpeg 8+ is required for Vulkan encoding; the project runs on FFmpeg 9.0.1.** Debian 13 ships
  7.1.5, which cannot open the Vulkan encoder. `/opt/ffmpeg9/bin/ffmpeg` is a source build of the
  upstream `release/9.0` branch (Vulkan + libx264 only, no vendor codecs); `/opt/ffmpeg8/bin/ffmpeg`
  remains as a static fallback. See finding #18.
- **Supported backends are Vulkan and CPU only.** NVENC and other vendor-specific paths were removed
  deliberately (#18).
- GPU acceleration: set **Hardware Acceleration → NVIDIA NVENC** in the Web UI (or
  `hwaccel: nvenc`). NVENC is verified working on the Tesla P4; Vulkan encode is not (finding #11b).
- Docker was not available on this host; `docker-compose.yml` was not exercised.

---

## Pre-existing backlog

### Security & Authentication
- [ ] Strengthen authentication with per-worker tokens, token rotation, and expiration
- [ ] Add audit logging for authentication events and sensitive API operations

### Testing
- [ ] Add unit tests for the master coordinator (`video-converter-master/internal/coordinator/`)
- [ ] Increase test coverage for the worker client package (only `progress_test.go` exists)

### Features
- [ ] Implement chunked/parallel file uploads for large video files
- [ ] Validate and test the resume-download feature (`enable_resume_download` exists but may not be exercised)
- [ ] Support additional output formats and encoding presets configurable per-job
- [ ] Add an admin API for dynamically adding/removing workers without restart
- [ ] Support compression during file transfer to reduce bandwidth usage

### Web UI
- [ ] Expand the Web UI beyond the dashboard — pages for job details, worker management, config editing

### Observability
- [ ] Expand Prometheus metrics — histograms for conversion duration, transfer speeds, queue depth
- [ ] Add Grafana dashboard templates or examples
- [ ] Implement structured error codes for API responses

### Code Quality
- [ ] Reduce cyclomatic complexity in larger functions (linter threshold is 20, which is high)
- [ ] Re-enable and address findings from currently disabled linters (~36 entries in `.golangci.yml`)
- [ ] Add package-level and exported-function Go doc comments where missing

### Deployment & Operations
- [ ] Add Kubernetes manifests or Helm chart for production deployment
- [ ] Add CI/CD pipeline for automated builds, tests, and container image publishing
- [ ] Create health-check and readiness probes suitable for orchestrators
- [ ] Add log rotation or log-shipping configuration examples

### Completed previously
- [x] Worker bearer-token auth, TLS support, checksum verification on transfers
- [x] Real CPU/memory metrics in heartbeats, surfaced in dashboard and `/api/stats`
- [x] Global queue pause/resume, job priority scheduling, fsnotify watch, bandwidth limiting
- [x] Real-time Web UI updates via SSE, job action controls, notification webhooks
- [x] Job deduplication by content hash (verified working — identical copy correctly skipped)
- [x] Architecture diagrams, API.md, CONTRIBUTING.md, CHANGELOG.md
