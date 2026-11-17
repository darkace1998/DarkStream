# Log rotation in DarkStream

Both binaries log with Go's `slog` via `utils.InitLogger(level, format, outputPath)`:

- **Default:** logs go to **stdout** (JSON or text, see `logging.format`).
- **Optional file:** set `logging.output_path` in `config.yaml` and logs are
  written to that file *in addition* to stdout (`io.MultiWriter`), with `0600`
  permissions, created append-only. There is **no built-in size/age rotation** —
  use the platform mechanism below.

## Kubernetes / containers (recommended)

Leave `logging.output_path` empty. The container runtime owns rotation:

- **containerd / CRI-O:** `/var/log/pods/*.log` are rotated by the runtime
  (default 10 MiB per file in containerd's CRI plugin).
- **Ship to Loki/ELK/Fluentd:** logs go to stdout, so any DaemonSet log agent
  picks them up; retention is configured in the collector, not here.
- JSON format (`logging.format: "json"`) is recommended in clusters so Loki can
  parse fields without regex.

## Host / systemd installs with `logging.output_path` set

Use `logrotate`. Example `/etc/logrotate.d/darkstream`:

```text
/var/log/darkstream/master.log /var/log/darkstream/worker.log {
    daily
    rotate 14
    maxsize 100M
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}
```

`copytruncate` is required because the processes keep the log file open and
append — `postrotate` signals do not reach them (slog holds the fd).

## systemd-journald (stdout mode on a host)

```bash
# /etc/systemd/journald.conf
[Journal]
SystemMaxUse=2G
MaxFileSec=7day
```

Then `journalctl -u darkstream-master --since today` etc.

## What to watch in logs

- `level=ERROR` — job failures, DB errors, failed chunk uploads/finalization.
- `level=WARN` — readiness failures (`no scan location running`), path
  validation rejections, audit write failures.
- Audit-relevant actions (config changes, queue pause/resume, job
  cancel/retry, worker admin, chunked uploads) are also persisted in the
  `audit_log` SQLite table and queryable via `GET /api/audit-log` — the
  database is the durable audit trail, logs are the human-readable stream.
