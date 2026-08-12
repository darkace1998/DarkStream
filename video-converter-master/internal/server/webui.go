package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
)

// workerOnlineThreshold defines how long since last heartbeat before a worker is considered offline
const workerOnlineThreshold = 2 * time.Minute

// jobLogTailScript is shared by the dashboard's log modal and the job detail
// page so both tail a running job identically. It is spliced into each template
// rather than duplicated, because the append-instead-of-redraw rule below is
// easy to get subtly wrong in one copy and not the other.
const jobLogTailScript = `
        // ---- Live job log ----
        //
        // A running job's log keeps growing while someone is reading it.
        // Redrawing the whole thing on every poll would wipe out any text
        // selection they had made — the defect that moved the log into a modal
        // in the first place — so new lines are appended and the lines already
        // on screen are left untouched.

        const logTailIntervalMs = 3000;
        const logBottomSlackPx = 40;

        function jobLogUrl(jobId) {
            let url = '/api/job/logs?job_id=' + encodeURIComponent(jobId);
            const apiKey = localStorage.getItem('darkstreamAdminApiKey');
            if (apiKey) { url += '&api_key=' + encodeURIComponent(apiKey); }
            return url;
        }

        // A timestamp and message together identify a line well enough to tell
        // whether what is on screen is still a prefix of what the server has.
        function logLineKey(entry) {
            return (entry.timestamp || '') + '\u0000' + (entry.message || '');
        }

        function buildLogLine(entry) {
            const line = document.createElement('div');
            line.className = 'log-line log-' + (entry.level || 'info');

            const ts = document.createElement('span');
            ts.className = 'log-ts';
            ts.textContent = new Date(entry.timestamp).toLocaleTimeString();

            const stage = document.createElement('span');
            stage.className = 'log-stage';
            stage.textContent = entry.stage || '';

            const msg = document.createElement('span');
            msg.className = 'log-msg';
            msg.textContent = entry.message;

            line.append(ts, stage, msg);
            return line;
        }

        function renderLogInto(container, entries, options) {
            options = options || {};
            entries = entries || [];

            if (!entries.length) {
                container.__logKeys = [];
                container.textContent = options.running
                    ? 'Waiting for the worker to report the first lines\u2026'
                    : 'No log recorded for this job. Logs are captured while a job runs, so jobs that ran before logging was added have none.';
                return;
            }

            const keys = container.__logKeys || [];

            // The worker's log drops its own middle once it outgrows its cap, so
            // what arrives is not always an extension of what is displayed. Only
            // then is a full redraw justified.
            let appendOnly = entries.length >= keys.length;
            if (appendOnly) {
                for (let i = 0; i < keys.length; i++) {
                    if (logLineKey(entries[i]) !== keys[i]) { appendOnly = false; break; }
                }
            }

            // Follow the tail only when the reader is already at the bottom.
            // Yanking the view down while they are reading further up would be
            // hostile, and a live log would become unreadable.
            const atBottom = container.scrollHeight - container.scrollTop - container.clientHeight <= logBottomSlackPx;

            let from = keys.length;
            if (!appendOnly || !keys.length) {
                container.textContent = ''; // also clears any placeholder text
                keys.length = 0;
                from = 0;
            }

            for (let i = from; i < entries.length; i++) {
                container.appendChild(buildLogLine(entries[i]));
                keys.push(logLineKey(entries[i]));
            }
            container.__logKeys = keys;

            if (atBottom) { container.scrollTop = container.scrollHeight; }
        }

        // tailJobLog loads a job's log and, while that job is still running,
        // keeps it current. It returns a function that stops the tail.
        function tailJobLog(container, jobId, options) {
            options = options || {};
            let stopped = false;
            let timer = null;

            function stop() {
                if (stopped) { return; }
                stopped = true;
                if (timer) { clearTimeout(timer); timer = null; }
                if (options.onStop) { options.onStop(); }
            }

            async function poll(isFirst) {
                if (stopped) { return; }
                try {
                    const res = await fetch(jobLogUrl(jobId));
                    if (!res.ok) { throw new Error('HTTP ' + res.status); }
                    const data = await res.json();
                    if (stopped) { return; }

                    renderLogInto(container, data.entries, {running: data.running});
                    if (options.onUpdate) { options.onUpdate(data); }

                    // The job has finished and this render included its closing
                    // lines, so there is nothing left to wait for.
                    if (!data.running) { stop(); return; }
                } catch (e) {
                    if (stopped) { return; }
                    // Only the first failure is worth showing: after that there
                    // is a readable log on screen, and a dropped poll is simply
                    // retried on the next tick.
                    if (isFirst) { container.textContent = 'Failed to load log: ' + e.message; }
                }
                if (!stopped) { timer = setTimeout(function() { poll(false); }, logTailIntervalMs); }
            }

            poll(true);
            return stop;
        }
`

// webUITemplate is the HTML template for the web interface
var webUITemplate = template.Must(template.New("webui").Parse(`<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" lang="en">
<head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>DarkStream - Video Converter Dashboard</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #1a1a2e; color: #eee; line-height: 1.6; }
        .container { max-width: 1400px; margin: 0 auto; padding: 20px; }
        h1 { color: #00d9ff; margin-bottom: 10px; }
        h2 { color: #00d9ff; margin: 20px 0 15px 0; font-size: 1.3em; border-bottom: 1px solid #333; padding-bottom: 10px; }
        .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; flex-wrap: wrap; gap: 10px; }
        .version { color: #888; font-size: 0.9em; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(350px, 1fr)); gap: 20px; }
        .card { background: #16213e; border-radius: 8px; padding: 20px; box-shadow: 0 2px 10px rgba(0,0,0,0.3); }
        .form-group { margin-bottom: 15px; }
        label { display: block; margin-bottom: 5px; color: #aaa; font-size: 0.9em; }
        input, select { width: 100%; padding: 10px 12px; border: 1px solid #333; border-radius: 4px; background: #0f0f23; color: #fff; font-size: 1em; }
        input:focus, select:focus { outline: none; border-color: #00d9ff; }
        select { cursor: pointer; }
        .btn { padding: 10px 20px; border: none; border-radius: 4px; cursor: pointer; font-size: 0.9em; transition: all 0.2s; margin-right: 5px; }
        .btn-primary { background: #00d9ff; color: #000; font-weight: bold; }
        .btn-primary:hover { background: #00b8d4; }
        .btn-danger { background: #dc3545; color: #fff; }
        .btn-danger:hover { background: #c82333; }
        .btn-sm { padding: 5px 10px; font-size: 0.8em; }
        .btn:disabled { background: #555; color: #888; cursor: not-allowed; }
        .status { padding: 10px 15px; border-radius: 4px; margin-bottom: 20px; }
        .status-success { background: #1b4332; color: #95d5b2; }
        .status-error { background: #5c2323; color: #f8d7da; }
        .status-info { background: #1e3a5f; color: #89cff0; }
        .table { width: 100%; border-collapse: collapse; margin-top: 10px; font-size: 0.9em; }
        .table th, .table td { padding: 10px 8px; text-align: left; border-bottom: 1px solid #333; }
        .table th { color: #aaa; font-weight: normal; font-size: 0.85em; }
        .badge { padding: 3px 8px; border-radius: 3px; font-size: 0.8em; }
        .badge-online { background: #1b4332; color: #95d5b2; }
        .badge-offline { background: #5c2323; color: #f8d7da; }
        .badge-pending { background: #2d4a6f; color: #89cff0; }
        .badge-processing { background: #614a1f; color: #ffc107; }
        .badge-completed { background: #1b4332; color: #95d5b2; }
        .badge-failed { background: #5c2323; color: #f8d7da; }
        .badge-success { background: #1b4332; color: #95d5b2; }
        .meta { color: #666; font-size: 0.85em; }
        .tabs { display: flex; border-bottom: 2px solid #333; margin-bottom: 20px; }
        .tab { padding: 12px 24px; cursor: pointer; color: #888; border-bottom: 2px solid transparent; margin-bottom: -2px; }
        .tab:hover { color: #00d9ff; }
        .tab.active { color: #00d9ff; border-bottom-color: #00d9ff; }
        .tab-content { display: none; }
        .tab-content.active { display: block; }
        a { color: #00d9ff; }
        .table code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
        .job-link { color: #00d9ff; text-decoration: none; border-bottom: 1px dotted #00d9ff; cursor: pointer; }
        .job-link:hover { color: #fff; border-bottom-color: #fff; }
        .progress-row { display: flex; align-items: center; gap: 8px; }
        .progress-pct { font-variant-numeric: tabular-nums; min-width: 42px; }
        .fps-text, .eta-text { font-variant-numeric: tabular-nums; white-space: nowrap; }
        .stage-text { text-transform: capitalize; }
        .live-badge { display: inline-flex; align-items: center; gap: 6px; background: #1b3a2a; border: 1px solid #2d6a4f; color: #95d5b2; border-radius: 10px; padding: 1px 9px; font-size: .75em; font-weight: 600; letter-spacing: .04em; text-transform: uppercase; margin-left: 10px; vertical-align: middle; }
        .live-dot { width: 7px; height: 7px; border-radius: 50%; background: #52b788; animation: live-pulse 1.6s ease-in-out infinite; }
        @keyframes live-pulse { 0%, 100% { opacity: 1; } 50% { opacity: .25; } }
        .log-view { background: #0f0f23; border-radius: 4px; padding: 12px; max-height: 380px; overflow: auto; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .8em; line-height: 1.5; }
        .joblog-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,.7); z-index: 1100; }
        .joblog-panel { background: #16213e; border-radius: 8px; box-shadow: 0 10px 40px rgba(0,0,0,.5); width: min(1100px, 92vw); max-height: 84vh; margin: 6vh auto 0; display: flex; flex-direction: column; overflow: hidden; }
        .joblog-header { display: flex; align-items: center; gap: 8px; padding: 14px 16px; border-bottom: 1px solid #2a3a5e; }
        .joblog-body { flex: 1 1 auto; max-height: none; margin: 14px; user-select: text; }
        .log-line { display: flex; gap: 10px; white-space: pre-wrap; word-break: break-word; }
        .log-ts { color: #666; flex: 0 0 auto; }
        .log-stage { color: #7aa2f7; flex: 0 0 70px; }
        .log-msg { flex: 1 1 auto; }
        .log-info .log-msg { color: #ddd; }
        .log-warn .log-msg { color: #ffd479; }
        .log-error .log-msg { color: #ff9d9d; }
        .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 15px; margin-bottom: 20px; }
        .stat-card { background: #16213e; border-radius: 8px; padding: 15px; text-align: center; }
        .stat-value { font-size: 2em; color: #00d9ff; font-weight: bold; }
        .stat-label { color: #888; font-size: 0.85em; }
        .no-data { color: #888; font-style: italic; padding: 30px; text-align: center; }
        .config-notice { background: #4a3a1a; border: 1px solid #7a5a2a; color: #ffd479; border-radius: 4px; padding: 12px 15px; margin-bottom: 15px; }
        .loading { display: none; }
        .loading.show { display: inline-block; margin-left: 10px; }
        @keyframes spin { to { transform: rotate(360deg); } }
        .spinner { width: 20px; height: 20px; border: 2px solid #333; border-top-color: #00d9ff; border-radius: 50%; animation: spin 0.8s linear infinite; display: inline-block; }
        .progress-bar { background: #333; border-radius: 4px; height: 6px; overflow: hidden; }
        .progress-fill { background: #00d9ff; height: 100%; transition: width 0.3s; }
        .truncate { max-width: 200px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .refresh-btn { float: right; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div>
                <h1>🎬 DarkStream Dashboard</h1>
                <span class="version">Video Converter System</span>
            </div>
            <div class="meta" style="display: flex; align-items: center; gap: 15px;">
                <div id="queue-status-container">
                    <span id="queue-status-badge" class="badge badge-success" style="padding: 5px 10px; font-size: 0.9em; margin-right: 10px;">Queue Active</span>
                    <button id="toggle-queue-btn" class="btn btn-sm" style="background: #e6a23c; color: #fff;" onclick="toggleQueue()">Pause Queue</button>
                </div>
                Config Version: {{.Config.Version}} | <span id="live-indicator">Live 🟢</span>
            </div>
        </div>

        <!-- Stats Overview -->
        <div class="stats-grid">
            <div class="stat-card">
                <div class="stat-value" id="stat-pending">{{.Stats.Pending}}</div>
                <div class="stat-label">Pending</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="stat-processing">{{.Stats.Processing}}</div>
                <div class="stat-label">Processing</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="stat-completed">{{.Stats.Completed}}</div>
                <div class="stat-label">Completed</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="stat-failed">{{.Stats.Failed}}</div>
                <div class="stat-label">Failed</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="stat-workers">{{.Stats.Workers}}</div>
                <div class="stat-label">Workers</div>
            </div>
        </div>

        <div id="status" class="status" style="display: none;"></div>

        {{if .APIKeyRequired}}
        <div class="card" style="margin-bottom: 20px;">
            <h2>🔐 Admin Access</h2>
            <div class="form-group">
                <label for="apiKeyInput">API Key</label>
                <input id="apiKeyInput" type="password" placeholder="Enter master API key" autocomplete="current-password" />
            </div>
            <button class="btn btn-primary" type="button" onclick="saveApiKey()">Save Key</button>
            <button class="btn btn-danger" type="button" onclick="clearApiKey()">Clear</button>
            <span class="meta">Stored locally in this browser.</span>
        </div>
        {{end}}

        <!-- Tab Navigation -->
        <div class="tabs">
            <div class="tab active" data-tab="queue">📋 Job Queue</div>
            <div class="tab" data-tab="processing">⚙️ Processing</div>
            <div class="tab" data-tab="history">📜 History</div>
            <div class="tab" data-tab="workers">👥 Workers</div>
            <div class="tab" data-tab="libraries">📚 Libraries</div>
            <div class="tab" data-tab="config">⚙️ Configuration</div>
        </div>

        <!-- Libraries Tab -->
        <div id="libraries" class="tab-content">
            <div class="card">
                <h2>Video Libraries
                    <button class="btn btn-sm refresh-btn" onclick="newLibrary()">+ Add Library</button>
                </h2>
                <p style="opacity:.75;margin-bottom:12px">
                    Each library is a folder tree that DarkStream scans for video files.
                    Add as many as you like &mdash; they all feed the same job queue.
                    Changes apply immediately; no restart required.
                </p>
                <table class="table">
                    <thead>
                        <tr>
                            <th>Name</th>
                            <th>Source Path</th>
                            <th>Output Path</th>
                            <th>Settings</th>
                            <th>Jobs</th>
                            <th>Status</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody id="libraries-tbody" data-empty="No libraries configured yet. Click &quot;Add Library&quot; to get started." data-cols="7">
                        <tr data-placeholder="1"><td colspan="7" class="no-data">Loading&hellip;</td></tr>
                    </tbody>
                </table>
            </div>

            <div class="card" id="library-editor" style="display:none">
                <h2 id="library-editor-title">Add Library</h2>
                <form id="libraryForm">
                    <input type="hidden" id="lib-id" />
                    <div class="grid">
                        <div>
                            <div class="form-group">
                                <label for="lib-name">Name</label>
                                <input type="text" id="lib-name" placeholder="e.g. Movies" required />
                            </div>
                            <div class="form-group">
                                <label for="lib-path">Source Path (absolute)</label>
                                <input type="text" id="lib-path" placeholder="/mnt/storage/movies" required />
                            </div>
                            <div class="form-group">
                                <label for="lib-output">Output Path (absolute)</label>
                                <input type="text" id="lib-output" placeholder="/mnt/storage/converted/movies" />
                            </div>
                        </div>
                        <div>
                            <div class="form-group">
                                <label for="lib-extensions">File Extensions (blank = use global)</label>
                                <input type="text" id="lib-extensions" placeholder=".mp4, .mkv, .avi" />
                            </div>
                            <div class="form-group">
                                <label for="lib-depth">Recursive Depth (blank = use global)</label>
                                <input type="number" id="lib-depth" placeholder="-1 = unlimited" />
                            </div>
                            <div class="form-group">
                                <label><input type="checkbox" id="lib-enabled" checked /> Enabled</label>
                            </div>
                            <div class="form-group">
                                <label>
                                    <input type="checkbox" id="lib-replace" />
                                    Replace source files (converts in place &mdash; no output path needed)
                                </label>
                            </div>
                        </div>
                    </div>
                    <h3 style="margin-top:18px">Conversion Settings</h3>
                    <p style="opacity:.75;margin-bottom:12px">
                        Leave a field on &ldquo;Use global&rdquo; (or blank) to inherit the global
                        Configuration setting. Anything you set here applies only to this library.
                    </p>
                    <div class="grid">
                        <div>
                            <div class="form-group">
                                <label for="lib-resolution">Resolution</label>
                                <select id="lib-resolution">
                                    <option value="">Use global</option>
                                    <option value="3840x2160">3840x2160 (4K)</option>
                                    <option value="2560x1440">2560x1440 (1440p)</option>
                                    <option value="1920x1080">1920x1080 (1080p)</option>
                                    <option value="1280x720">1280x720 (720p)</option>
                                    <option value="854x480">854x480 (480p)</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label for="lib-codec">Video Codec</label>
                                <select id="lib-codec">
                                    <option value="">Use global</option>
                                    <option value="h264">H.264 (AVC)</option>
                                    <option value="h265">H.265 (HEVC)</option>
                                    <option value="vp9">VP9</option>
                                    <option value="av1">AV1</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label for="lib-bitrate">Video Bitrate</label>
                                <input type="text" id="lib-bitrate" placeholder="blank = global (e.g. 5M)" />
                            </div>
                            <div class="form-group">
                                <label for="lib-preset">Encoding Preset</label>
                                <select id="lib-preset">
                                    <option value="">Use global</option>
                                    <option value="ultrafast">Ultrafast</option>
                                    <option value="veryfast">Veryfast</option>
                                    <option value="fast">Fast</option>
                                    <option value="medium">Medium</option>
                                    <option value="slow">Slow</option>
                                    <option value="veryslow">Veryslow</option>
                                </select>
                            </div>
                        </div>
                        <div>
                            <div class="form-group">
                                <label for="lib-format">Output Format</label>
                                <select id="lib-format">
                                    <option value="">Use global</option>
                                    <option value="mp4">MP4</option>
                                    <option value="mkv">MKV</option>
                                    <option value="webm">WebM</option>
                                    <option value="avi">AVI</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label for="lib-audio-codec">Audio Codec</label>
                                <select id="lib-audio-codec">
                                    <option value="">Use global</option>
                                    <option value="aac">AAC</option>
                                    <option value="mp3">MP3</option>
                                    <option value="opus">Opus</option>
                                    <option value="vorbis">Vorbis</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label for="lib-audio-bitrate">Audio Bitrate</label>
                                <input type="text" id="lib-audio-bitrate" placeholder="blank = global (e.g. 128k)" />
                            </div>
                            <div class="form-group">
                                <label for="lib-hwaccel">Hardware Acceleration</label>
                                <select id="lib-hwaccel">
                                    <option value="">Use global</option>
                                    <option value="none">None (CPU)</option>
                                    <option value="vulkan">Vulkan</option>
                                </select>
                            </div>
                        </div>
                    </div>

                    <button type="submit" class="btn">Save Library</button>
                    <button type="button" class="btn btn-danger" onclick="cancelLibraryEdit()">Cancel</button>
                    <span id="library-status" style="margin-left:12px"></span>
                </form>
            </div>
        </div>

        <!-- Queue Tab -->
        <div id="queue" class="tab-content active">
            <div class="card">
                <h2>Pending Jobs <button class="btn btn-danger btn-sm refresh-btn" onclick="cancelAllPending()">Cancel All Pending</button></h2>
                <table class="table">
                    <thead>
                        <tr>
                            <th>Job ID</th>
                            <th>Source</th>
                            <th>Priority</th>
                            <th>Created</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody id="pending-jobs" data-empty="No pending jobs in queue" data-cols="5">
                        {{if not .PendingJobs}}
                        <tr data-placeholder="1"><td colspan="5" class="no-data">No pending jobs in queue</td></tr>
                        {{end}}
                        {{range .PendingJobs}}
                        <tr>
                            <td><a class="job-link" href="/ui/job?id={{.ID}}" title="Open job details"><code>{{.ID}}</code></a></td>
                            <td class="truncate" title="{{.SourcePath}}">{{.SourcePath}}</td>
                            <td><span class="badge badge-pending">{{.Priority}}</span></td>
                            <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
                            <td>
                                <button class="btn btn-primary btn-sm" onclick="updatePriority('{{.ID}}', {{.Priority}})">Priority</button>
                                <button class="btn btn-danger btn-sm" onclick="cancelJob('{{.ID}}')">Cancel</button>
                            </td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
            </div>
        </div>

        <!-- Processing Tab -->
        <div id="processing" class="tab-content">
            <div class="card">
                <h2>Currently Processing</h2>
                <table class="table">
                    <thead>
                        <tr>
                            <th>Job ID</th>
                            <th>Library</th>
                            <th>File</th>
                            <th>Stage</th>
                            <th>Progress</th>
                            <th>FPS</th>
                            <th>ETA</th>
                            <th>Worker</th>
                            <th>Started</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody id="processing-jobs" data-empty="No jobs currently processing" data-cols="10">
                        {{if not .ProcessingJobs}}
                        <tr data-placeholder="1"><td colspan="10" class="no-data">No jobs currently processing</td></tr>
                        {{end}}
                        {{range .ProcessingJobs}}
                        <tr>
                            <td><a class="job-link" href="/ui/job?id={{.ID}}" title="Open job details"><code>{{.ID}}</code></a></td>
                            <td><span class="badge badge-pending">{{.LibraryName}}</span></td>
                            <td class="truncate" title="{{.SourcePath}}">{{.RelPath}}</td>
                            <td><span class="stage-text">&mdash;</span></td>
                            <td>
                                <div class="progress-row">
                                    <div class="progress-bar" style="width: 100px;">
                                        <div class="progress-fill" style="width: 0%;" id="progress-{{.ID}}"></div>
                                    </div>
                                    <span class="progress-pct">0%</span>
                                </div>
                            </td>
                            <td class="fps-text">&mdash;</td>
                            <td class="eta-text">&mdash;</td>
                            <td>{{.WorkerID}}</td>
                            <td>{{if .StartedAt}}{{.StartedAt.Format "15:04:05"}}{{end}}</td>
                            <td>
                                <button class="btn btn-secondary btn-sm" onclick="openJobLogModal('{{.ID}}')">Log</button>
                                <button class="btn btn-danger btn-sm" onclick="cancelJob('{{.ID}}')">Cancel</button>
                            </td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
            </div>
        </div>

        <!-- History Tab -->
        <div id="history" class="tab-content">
            <div class="card">
                <h2>Recent Jobs (Last 50)
                    <button class="btn btn-danger btn-sm refresh-btn" onclick="pruneJobs('all')" style="margin-left: 10px;">Clear Completed & Failed</button>
                    <button class="btn btn-danger btn-sm refresh-btn" onclick="retryAllFailed()">Retry All Failed</button>
                </h2>
                <table class="table">
                    <thead>
                        <tr>
                            <th>Job ID</th>
                            <th>Library</th>
                            <th>File</th>
                            <th>Status</th>
                            <th>Worker</th>
                            <th>Duration</th>
                            <th>Completed</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody id="recent-jobs" data-empty="No recent jobs" data-cols="8">
                        {{if not .RecentJobs}}
                        <tr data-placeholder="1"><td colspan="8" class="no-data">No recent jobs</td></tr>
                        {{end}}
                        {{range .RecentJobs}}
                        <tr>
                            <td><a class="job-link" href="/ui/job?id={{.ID}}" title="Open job details"><code>{{.ID}}</code></a></td>
                            <td><span class="badge badge-pending">{{.LibraryName}}</span></td>
                            <td class="truncate" title="{{.SourcePath}}">{{.RelPath}}</td>
                            <td><span class="badge {{if eq .Status "completed"}}badge-completed{{else if eq .Status "failed"}}badge-failed{{else}}badge-pending{{end}}">{{.Status}}</span></td>
                            <td>{{.WorkerID}}</td>
                            <td>{{if .SourceDuration}}{{printf "%.0f" .SourceDuration}}s{{else}}-{{end}}</td>
                            <td>{{if .CompletedAt}}{{.CompletedAt.Format "2006-01-02 15:04"}}{{else}}-{{end}}</td>
                            <td>
                                <button class="btn btn-secondary btn-sm" onclick="openJobLogModal('{{.ID}}')">Log</button>
                                {{if eq .Status "failed"}}<button class="btn btn-primary btn-sm" onclick="retryJob('{{.ID}}')">Retry</button>{{end}}
                                {{if or (eq .Status "completed") (eq .Status "failed") (eq .Status "cancelled")}}
                                <button class="btn btn-secondary btn-sm" onclick="requeueJob('{{.ID}}')">Re-queue</button>
                                {{end}}
                            </td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
            </div>
        </div>

        <!-- Workers Tab -->
        <div id="workers" class="tab-content">
            <div class="card">
                <h2>🔗 Connect New Workers</h2>
                <p style="color: #aaa; margin-bottom: 15px;">Workers can connect to this master using the following command:</p>
                <div style="background: #0f0f23; padding: 15px; border-radius: 4px; font-family: monospace; color: #00d9ff; margin-bottom: 20px;">
                    worker -url {{.MasterURL}}
                </div>
                <p style="color: #888; font-size: 0.85em;">Workers will automatically receive their configuration from the master. Use the "Configure" button to set per-worker settings.</p>
            </div>
            <div class="card" style="margin-top: 20px;">
                <h2>🧪 Detected Encoding Capabilities
                    <button class="btn btn-sm refresh-btn" onclick="loadCapabilities()">Refresh</button>
                </h2>
                <p style="opacity:.75;margin-bottom:12px">
                    Each worker verifies these at startup by running a real test encode through the
                    same pipeline it uses for jobs &mdash; so a backend marked working here will work
                    for real conversions, and one marked unusable would fail every job.
                </p>
                <table class="table">
                    <thead>
                        <tr>
                            <th>Worker</th>
                            <th>GPU</th>
                            <th>Backend</th>
                            <th>Codec</th>
                            <th>Encoder</th>
                            <th>Result</th>
                            <th>Detail</th>
                        </tr>
                    </thead>
                    <tbody id="capabilities-tbody" data-empty="No worker has reported capabilities yet. They are probed when a worker starts." data-cols="7">
                        <tr data-placeholder="1"><td colspan="7" class="no-data">Loading&hellip;</td></tr>
                    </tbody>
                </table>
            </div>

            <div class="card" style="margin-top: 20px;">
                <h2>👥 Connected Workers</h2>
                <table class="table">
                    <thead>
                        <tr>
                            <th>Worker ID</th>
                            <th>Hostname</th>
                            <th>GPU</th>
                            <th>Active Jobs</th>
                            <th>CPU</th>
                            <th>Memory</th>
                            <th>Status</th>
                            <th>Last Seen</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody id="workers-list" data-empty="No workers connected" data-cols="9">
                        {{if not .Workers}}
                        <tr data-placeholder="1"><td colspan="9" class="no-data">No workers connected</td></tr>
                        {{end}}
                        {{range .Workers}}
                        <tr>
                            <td><code>{{.WorkerID}}</code></td>
                            <td>{{.Hostname}}</td>
                            <td>{{.GPU}}</td>
                            <td>{{.ActiveJobs}}</td>
                            <td>{{printf "%.1f" .CPUUsage}}%</td>
                            <td>{{printf "%.1f" .MemoryUsage}}%</td>
                            <td><span class="badge {{if .IsOnline}}badge-online{{else}}badge-offline{{end}}">{{if .IsOnline}}Online{{else}}Offline{{end}}</span></td>
                            <td>{{.LastHeartbeat.Format "15:04:05"}}</td>
                            <td><button class="btn btn-primary btn-sm" onclick="openWorkerSettings('{{.WorkerID}}')">⚙️ Configure</button></td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
            </div>
        </div>

        <!-- Worker Settings Modal -->
        <div id="jobLogModal" class="joblog-backdrop" style="display:none" onclick="if (event.target === this) closeJobLogModal()">
            <div class="joblog-panel">
                <div class="joblog-header">
                    <strong id="jobLogModalTitle">Job Log</strong>
                    <span id="jobLogLive" class="live-badge" style="display:none"><span class="live-dot"></span>live</span>
                    <span style="flex:1"></span>
                    <span id="jobLogCopyStatus" style="color:#95d5b2;font-size:.85em;margin-right:8px"></span>
                    <button class="btn btn-secondary btn-sm" onclick="copyJobLog()">Copy</button>
                    <a id="jobLogModalLink" class="btn btn-secondary btn-sm" href="#">Open job</a>
                    <button class="btn btn-danger btn-sm" onclick="closeJobLogModal()">Close</button>
                </div>
                <div id="jobLogModalBody" class="log-view joblog-body">Loading&hellip;</div>
            </div>
        </div>

        <div id="workerModal" style="display: none; position: fixed; top: 0; left: 0; width: 100%; height: 100%; background: rgba(0,0,0,0.7); z-index: 1000;">
            <div style="position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%); background: #16213e; padding: 30px; border-radius: 8px; max-width: 600px; width: 90%; max-height: 80vh; overflow-y: auto;">
                <h2 style="margin-bottom: 20px;">⚙️ Worker Settings: <span id="modalWorkerId" style="color: #00d9ff;"></span></h2>
                <form id="workerSettingsForm">
                    <input type="hidden" id="wsWorkerId" />
                    <div class="grid">
                        <div>
                            <div class="form-group">
                                <label for="wsConcurrency">Concurrency</label>
                                <input type="number" id="wsConcurrency" min="1" max="16" value="3" />
                            </div>
                            <div class="form-group">
                                <label for="wsHeartbeat">Heartbeat Interval (s)</label>
                                <input type="number" id="wsHeartbeat" min="5" max="300" value="30" />
                            </div>
                            <div class="form-group">
                                <label for="wsJobCheck">Job Check Interval (s)</label>
                                <input type="number" id="wsJobCheck" min="1" max="60" value="5" />
                            </div>
                            <div class="form-group">
                                <label for="wsJobTimeout">Job Timeout (s)</label>
                                <input type="number" id="wsJobTimeout" min="60" max="86400" value="7200" />
                            </div>
                            <div class="form-group">
                                <label for="wsUseVulkan">Use Vulkan GPU</label>
                                <select id="wsUseVulkan">
                                    <option value="true">Enabled</option>
                                    <option value="false">Disabled</option>
                                </select>
                            </div>
                        </div>
                        <div>
                            <div class="form-group">
                                <label for="wsDownloadTimeout">Download Timeout (s)</label>
                                <input type="number" id="wsDownloadTimeout" min="60" max="86400" value="1800" />
                            </div>
                            <div class="form-group">
                                <label for="wsUploadTimeout">Upload Timeout (s)</label>
                                <input type="number" id="wsUploadTimeout" min="60" max="86400" value="1800" />
                            </div>
                            <div class="form-group">
                                <label for="wsMaxCacheSize">Max Cache Size (bytes)</label>
                                <input type="number" id="wsMaxCacheSize" min="0" value="10737418240" />
                            </div>
                            <div class="form-group">
                                <label for="wsLogLevel">Log Level</label>
                                <select id="wsLogLevel">
                                    <option value="debug">Debug</option>
                                    <option value="info" selected>Info</option>
                                    <option value="warn">Warn</option>
                                    <option value="error">Error</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label for="wsBandwidthLimit">Bandwidth Limit (bytes/s)</label>
                                <input type="number" id="wsBandwidthLimit" min="0" value="0" />
                            </div>
                        </div>
                    </div>
                    <div style="margin-top: 20px; display: flex; gap: 10px;">
                        <button type="submit" class="btn btn-primary">💾 Save Settings</button>
                        <button type="button" class="btn btn-danger" onclick="resetWorkerSettings()">🔄 Reset to Defaults</button>
                        <button type="button" class="btn" style="background: #555;" onclick="closeWorkerModal()">Cancel</button>
                    </div>
                </form>
                <div id="workerModalStatus" class="status" style="display: none; margin-top: 15px;"></div>
            </div>
        </div>

        <!-- Configuration Tab -->
        <div id="config" class="tab-content">
            <div id="config-changed-notice" class="config-notice" style="display:none">
                These settings were changed elsewhere. Reload the page to see the current values —
                saving this form now would overwrite them.
                <button class="btn btn-sm" onclick="location.reload()">Reload</button>
            </div>
            <form id="configForm">
                <div class="grid">
                    <div class="card">
                        <h2>Video Settings</h2>
                        <div class="form-group">
                            <label for="resolution">Resolution</label>
                            <select id="resolution" name="resolution">
                                <option value="1920x1080" {{if eq .Config.Video.Resolution "1920x1080"}}selected{{end}}>1920x1080 (1080p)</option>
                                <option value="1280x720" {{if eq .Config.Video.Resolution "1280x720"}}selected{{end}}>1280x720 (720p)</option>
                                <option value="854x480" {{if eq .Config.Video.Resolution "854x480"}}selected{{end}}>854x480 (480p)</option>
                                <option value="640x360" {{if eq .Config.Video.Resolution "640x360"}}selected{{end}}>640x360 (360p)</option>
                                <option value="3840x2160" {{if eq .Config.Video.Resolution "3840x2160"}}selected{{end}}>3840x2160 (4K)</option>
                                <option value="2560x1440" {{if eq .Config.Video.Resolution "2560x1440"}}selected{{end}}>2560x1440 (1440p)</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label for="codec">Video Codec</label>
                            <select id="codec" name="codec">
                                <option value="h264" {{if eq .Config.Video.Codec "h264"}}selected{{end}}>H.264 (AVC)</option>
                                <option value="h265" {{if eq .Config.Video.Codec "h265"}}selected{{end}}>H.265 (HEVC)</option>
                                <option value="vp9" {{if eq .Config.Video.Codec "vp9"}}selected{{end}}>VP9</option>
                                <option value="av1" {{if eq .Config.Video.Codec "av1"}}selected{{end}}>AV1</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label for="bitrate">Video Bitrate</label>
                            <input type="text" id="bitrate" name="bitrate" value="{{.Config.Video.Bitrate}}" placeholder="e.g., 5M, 8M, 2000k" />
                        </div>
                        <div class="form-group">
                            <label for="preset">Encoding Preset</label>
                            <select id="preset" name="preset">
                                <option value="ultrafast" {{if eq .Config.Video.Preset "ultrafast"}}selected{{end}}>Ultrafast</option>
                                <option value="superfast" {{if eq .Config.Video.Preset "superfast"}}selected{{end}}>Superfast</option>
                                <option value="veryfast" {{if eq .Config.Video.Preset "veryfast"}}selected{{end}}>Veryfast</option>
                                <option value="faster" {{if eq .Config.Video.Preset "faster"}}selected{{end}}>Faster</option>
                                <option value="fast" {{if eq .Config.Video.Preset "fast"}}selected{{end}}>Fast</option>
                                <option value="medium" {{if eq .Config.Video.Preset "medium"}}selected{{end}}>Medium</option>
                                <option value="slow" {{if eq .Config.Video.Preset "slow"}}selected{{end}}>Slow</option>
                                <option value="slower" {{if eq .Config.Video.Preset "slower"}}selected{{end}}>Slower</option>
                                <option value="veryslow" {{if eq .Config.Video.Preset "veryslow"}}selected{{end}}>Veryslow</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label for="hwaccel">Hardware Acceleration</label>
                            <select id="hwaccel" name="hwaccel">
                                <option value="none" {{if eq .Config.Video.HWAccel "none"}}selected{{end}}>None (CPU)</option>
                                <option value="vulkan" {{if eq .Config.Video.HWAccel "vulkan"}}selected{{end}}>Vulkan</option>
                            </select>
                        </div>
                    </div>
                    <div class="card">
                        <h2>Audio &amp; Output</h2>
                        <div class="form-group">
                            <label for="audioCodec">Audio Codec</label>
                            <select id="audioCodec" name="audioCodec">
                                <option value="aac" {{if eq .Config.Audio.Codec "aac"}}selected{{end}}>AAC</option>
                                <option value="mp3" {{if eq .Config.Audio.Codec "mp3"}}selected{{end}}>MP3</option>
                                <option value="opus" {{if eq .Config.Audio.Codec "opus"}}selected{{end}}>Opus</option>
                                <option value="vorbis" {{if eq .Config.Audio.Codec "vorbis"}}selected{{end}}>Vorbis</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label for="audioBitrate">Audio Bitrate</label>
                            <input type="text" id="audioBitrate" name="audioBitrate" value="{{.Config.Audio.Bitrate}}" placeholder="e.g., 128k, 192k, 320k" />
                        </div>
                        <div class="form-group">
                            <label for="format">Container Format</label>
                            <select id="format" name="format">
                                <option value="mp4" {{if eq .Config.Output.Format "mp4"}}selected{{end}}>MP4</option>
                                <option value="mkv" {{if eq .Config.Output.Format "mkv"}}selected{{end}}>MKV</option>
                                <option value="webm" {{if eq .Config.Output.Format "webm"}}selected{{end}}>WebM</option>
                                <option value="avi" {{if eq .Config.Output.Format "avi"}}selected{{end}}>AVI</option>
                            </select>
                        </div>
                    </div>
                </div>
                
                <h2 style="margin-top: 30px;">⚙️ Worker Settings</h2>
                <div class="grid">
                    <div class="card">
                        <h2>Processing</h2>
                        <div class="form-group">
                            <label for="concurrency">Concurrent Jobs per Worker</label>
                            <input type="number" id="concurrency" name="concurrency" value="{{.Config.Worker.Concurrency}}" min="1" max="16" />
                        </div>
                        <div class="form-group">
                            <label for="heartbeatInterval">Heartbeat Interval (seconds)</label>
                            <input type="number" id="heartbeatInterval" name="heartbeatInterval" value="{{.Config.Worker.HeartbeatInterval}}" min="5" max="300" />
                        </div>
                        <div class="form-group">
                            <label for="jobCheckInterval">Job Check Interval (seconds)</label>
                            <input type="number" id="jobCheckInterval" name="jobCheckInterval" value="{{.Config.Worker.JobCheckInterval}}" min="1" max="60" />
                        </div>
                        <div class="form-group">
                            <label for="jobTimeout">Job Timeout (seconds)</label>
                            <input type="number" id="jobTimeout" name="jobTimeout" value="{{.Config.Worker.JobTimeout}}" min="60" max="86400" />
                        </div>
                        <div class="form-group">
                            <label for="ffmpegTimeout">FFmpeg Timeout (seconds)</label>
                            <input type="number" id="ffmpegTimeout" name="ffmpegTimeout" value="{{.Config.Worker.FFmpegTimeout}}" min="60" max="86400" />
                        </div>
                    </div>
                    <div class="card">
                        <h2>Transfers &amp; Storage</h2>
                        <div class="form-group">
                            <label for="downloadTimeout">Download Timeout (seconds)</label>
                            <input type="number" id="downloadTimeout" name="downloadTimeout" value="{{.Config.Worker.DownloadTimeout}}" min="60" max="86400" />
                        </div>
                        <div class="form-group">
                            <label for="uploadTimeout">Upload Timeout (seconds)</label>
                            <input type="number" id="uploadTimeout" name="uploadTimeout" value="{{.Config.Worker.UploadTimeout}}" min="60" max="86400" />
                        </div>
                        <div class="form-group">
                            <label for="maxCacheSize">Max Cache Size (bytes)</label>
                            <input type="number" id="maxCacheSize" name="maxCacheSize" value="{{.Config.Worker.MaxCacheSize}}" min="0" />
                        </div>
                        <div class="form-group">
                            <label for="bandwidthLimit">Bandwidth Limit (bytes/s, 0=unlimited)</label>
                            <input type="number" id="bandwidthLimit" name="bandwidthLimit" value="{{.Config.Worker.BandwidthLimit}}" min="0" />
                        </div>
                        <div class="form-group">
                            <label for="cacheCleanupAge">Cache Cleanup Age (seconds)</label>
                            <input type="number" id="cacheCleanupAge" name="cacheCleanupAge" value="{{.Config.Worker.CacheCleanupAge}}" min="3600" max="604800" />
                        </div>
                        <div class="form-group">
                            <label for="enableResumeDownload">Resume Downloads</label>
                            <select id="enableResumeDownload" name="enableResumeDownload">
                                <option value="true" {{if .Config.Worker.EnableResumeDownload}}selected{{end}}>Enabled</option>
                                <option value="false" {{if not .Config.Worker.EnableResumeDownload}}selected{{end}}>Disabled</option>
                            </select>
                        </div>
                    </div>
                    <div class="card">
                        <h2>Hardware &amp; Logging</h2>
                        <div class="form-group">
                            <label for="useVulkan">Use Vulkan GPU Acceleration</label>
                            <select id="useVulkan" name="useVulkan">
                                <option value="true" {{if .Config.Worker.UseVulkan}}selected{{end}}>Enabled</option>
                                <option value="false" {{if not .Config.Worker.UseVulkan}}selected{{end}}>Disabled</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label for="logLevel">Log Level</label>
                            <select id="logLevel" name="logLevel">
                                <option value="debug" {{if eq .Config.Worker.LogLevel "debug"}}selected{{end}}>Debug</option>
                                <option value="info" {{if eq .Config.Worker.LogLevel "info"}}selected{{end}}>Info</option>
                                <option value="warn" {{if eq .Config.Worker.LogLevel "warn"}}selected{{end}}>Warn</option>
                                <option value="error" {{if eq .Config.Worker.LogLevel "error"}}selected{{end}}>Error</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label for="logFormat">Log Format</label>
                            <select id="logFormat" name="logFormat">
                                <option value="json" {{if eq .Config.Worker.LogFormat "json"}}selected{{end}}>JSON</option>
                                <option value="text" {{if eq .Config.Worker.LogFormat "text"}}selected{{end}}>Text</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label for="maxAPIRequestsPerMin">Max API Requests/Minute</label>
                            <input type="number" id="maxAPIRequestsPerMin" name="maxAPIRequestsPerMin" value="{{.Config.Worker.MaxAPIRequestsPerMin}}" min="1" max="1000" />
                        </div>
                    </div>
                </div>
                
                <div style="margin-top: 20px;">
                    <button type="submit" class="btn btn-primary" id="saveBtn">💾 Save Configuration</button>
                    <div class="loading" id="loading"><span class="spinner"></span></div>
                </div>
            </form>
        </div>
    </div>

    <script>
        const apiKeyStorageKey = 'darkstreamAdminApiKey';

        function loadApiKey() {
            const input = document.getElementById('apiKeyInput');
            if (!input) {
                return;
            }
            const saved = localStorage.getItem(apiKeyStorageKey) || '';
            input.value = saved;
        }

        function saveApiKey() {
            const input = document.getElementById('apiKeyInput');
            if (!input) {
                return;
            }
            const value = input.value.trim();
            if (value) {
                localStorage.setItem(apiKeyStorageKey, value);
            } else {
                localStorage.removeItem(apiKeyStorageKey);
            }
        }

        function clearApiKey() {
            localStorage.removeItem(apiKeyStorageKey);
            const input = document.getElementById('apiKeyInput');
            if (input) {
                input.value = '';
            }
        }

        function authHeaders(headers) {
            const out = {};
            if (headers) {
                Object.keys(headers).forEach(function(key) {
                    out[key] = headers[key];
                });
            }
            const apiKey = localStorage.getItem(apiKeyStorageKey);
            if (apiKey) {
                out.Authorization = 'Bearer ' + apiKey;
            }
            return out;
        }

        async function authorizedFetch(url, options) {
            const opts = options || {};
            opts.headers = authHeaders(opts.headers);
            return fetch(url, opts);
        }

        // Tab switching
        document.querySelectorAll('.tab').forEach(function(tab) {
            tab.addEventListener('click', function() {
                document.querySelectorAll('.tab').forEach(function(t) { t.classList.remove('active'); });
                document.querySelectorAll('.tab-content').forEach(function(c) { c.classList.remove('active'); });
                this.classList.add('active');
                document.getElementById(this.dataset.tab).classList.add('active');
            });
        });

        loadApiKey();

        // Config form submission
        document.getElementById('configForm').addEventListener('submit', async function(e) {
            e.preventDefault();
            const btn = document.getElementById('saveBtn');
            const loading = document.getElementById('loading');
            const status = document.getElementById('status');
            
            btn.disabled = true;
            loading.classList.add('show');
            
            const config = {
                video: {
                    resolution: document.getElementById('resolution').value,
                    codec: document.getElementById('codec').value,
                    bitrate: document.getElementById('bitrate').value,
                    preset: document.getElementById('preset').value,
                    hwaccel: document.getElementById('hwaccel').value
                },
                audio: {
                    codec: document.getElementById('audioCodec').value,
                    bitrate: document.getElementById('audioBitrate').value
                },
                output: { format: document.getElementById('format').value },
                worker: {
                    concurrency: parseInt(document.getElementById('concurrency').value) || 3,
                    heartbeat_interval: parseInt(document.getElementById('heartbeatInterval').value) || 30,
                    job_check_interval: parseInt(document.getElementById('jobCheckInterval').value) || 5,
                    job_timeout: parseInt(document.getElementById('jobTimeout').value) || 7200,
                    max_api_requests_per_min: parseInt(document.getElementById('maxAPIRequestsPerMin').value) || 60,
                    download_timeout: parseInt(document.getElementById('downloadTimeout').value) || 1800,
                    upload_timeout: parseInt(document.getElementById('uploadTimeout').value) || 1800,
                    max_cache_size: parseInt(document.getElementById('maxCacheSize').value) || 10737418240,
                    cache_cleanup_age: parseInt(document.getElementById('cacheCleanupAge').value) || 86400,
                    bandwidth_limit: parseInt(document.getElementById('bandwidthLimit').value) || 0,
                    enable_resume_download: document.getElementById('enableResumeDownload').value === 'true',
                    use_vulkan: document.getElementById('useVulkan').value === 'true',
                    ffmpeg_timeout: parseInt(document.getElementById('ffmpegTimeout').value) || 7200,
                    log_level: document.getElementById('logLevel').value,
                    log_format: document.getElementById('logFormat').value
                }
            };
            
            try {
                const resp = await authorizedFetch('/api/config', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(config)
                });
                const data = await resp.json();
                status.className = resp.ok ? 'status status-success' : 'status status-error';
                status.textContent = resp.ok ? 'Configuration saved!' : 'Error: ' + (data.error || 'Unknown error');
            } catch (err) {
                status.className = 'status status-error';
                status.textContent = 'Network error: ' + err.message;
            }
            
            status.style.display = 'block';
            btn.disabled = false;
            loading.classList.remove('show');
            setTimeout(function() { status.style.display = 'none'; }, 3000);
        });

        // Update job priority
        async function updatePriority(jobId, currentPriority) {
            const newPriorityStr = prompt('Enter new priority for job ' + jobId + ' (0-10):', currentPriority);
            if (newPriorityStr === null) return;

            const newPriority = parseInt(newPriorityStr, 10);
            if (isNaN(newPriority) || newPriority < 0 || newPriority > 10) {
                alert('Invalid priority. Must be a number between 0 and 10.');
                return;
            }

            try {
                const resp = await authorizedFetch('/api/job/priority', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ job_id: jobId, priority: newPriority })
                });

                if (resp.ok) {
                    location.reload();
                } else {
                    const data = await resp.json().catch(() => ({}));
                    alert('Failed to update priority: ' + (data.error || resp.statusText));
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        // Cancel single job
        async function cancelJob(jobId) {
            if (!confirm('Cancel job ' + jobId + '?')) return;
            try {
                const resp = await authorizedFetch('/api/job/cancel?job_id=' + encodeURIComponent(jobId), { method: 'POST' });
                if (resp.ok) {
                    location.reload();
                } else {
                    alert('Failed to cancel job');
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        // Prune jobs
        async function pruneJobs(status) {
            let msg = 'Are you sure you want to clear ' + status + ' jobs?';
            if (status === 'all') {
                msg = 'Are you sure you want to clear all completed, failed and cancelled jobs?';
            }
            if (!confirm(msg)) return;

            try {
                const response = await authorizedFetch('/api/jobs/prune?status=' + status, {
                    method: 'DELETE'
                });
                if (!response.ok) {
                    throw new Error('Request failed with status ' + response.status);
                }
                const data = await response.json();
                alert('Cleared ' + data.deleted_count + ' jobs');
                window.location.reload();
            } catch (error) {
                console.error('Error pruning jobs:', error);
                alert('Error: ' + error.message);
            }
        }

        // Cancel all pending
        async function cancelAllPending() {
            if (!confirm('Cancel ALL pending jobs?')) return;
            try {
                const resp = await authorizedFetch('/api/jobs/cancel?status=pending', { method: 'POST' });
                if (resp.ok) {
                    const data = await resp.json();
                    alert('Cancelled ' + data.cancelled_count + ' jobs');
                    location.reload();
                } else {
                    alert('Failed to cancel jobs');
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        async function retryJob(jobId) {
            if (!confirm('Retry job ' + jobId + '?')) return;
            try {
                const resp = await authorizedFetch('/api/job/retry?job_id=' + encodeURIComponent(jobId), { method: 'POST' });
                if (resp.ok) {
                    location.reload();
                } else {
                    alert('Failed to retry job');
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        async function requeueJob(jobId) {
            if (!confirm('Re-queue job ' + jobId + '?')) return;
            try {
                const resp = await authorizedFetch('/api/job/requeue?job_id=' + encodeURIComponent(jobId), { method: 'POST' });
                if (resp.ok) {
                    location.reload();
                } else {
                    alert('Failed to re-queue job');
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        async function retryAllFailed() {
            if (!confirm('Retry ALL failed jobs?')) return;
            try {
                const resp = await authorizedFetch('/api/retry', { method: 'POST' });
                if (resp.ok) {
                    location.reload();
                } else {
                    alert('Failed to retry jobs');
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        // Connect to stats stream for real-time updates
        // Renders a remaining-time estimate. The server sends 0 when a job has
        // not run long enough to extrapolate from, in which case nothing is shown
        // rather than a misleading number.
        function formatEta(seconds) {
            if (!seconds || seconds <= 0) { return '—'; }
            const s = Math.round(seconds);
            if (s < 60) { return s + 's'; }
            const m = Math.floor(s / 60);
            if (m < 60) { return m + 'm ' + (s % 60) + 's'; }
            return Math.floor(m / 60) + 'h ' + (m % 60) + 'm';
        }

` + jobLogTailScript + `
        // ---- Job log viewer ----

        // The log is shown in a modal rather than inline in the History table on
        // purpose: that table is rebuilt wholesale every time a stats event
        // arrives, which destroyed any text selection inside an inline log (and
        // could leave the browser's selection anchors pointing at removed nodes).
        // The modal lives outside the table, so refreshes never touch it.
        let jobLogModalJobId = null;
        let stopJobLogTail = null;

        function openJobLogModal(jobId) {
            // Stop tailing whatever was open before, or two tails would write
            // into the same box.
            stopJobLogTailIfRunning();

            jobLogModalJobId = jobId;

            document.getElementById('jobLogModalTitle').textContent = 'Job Log — ' + jobId;
            document.getElementById('jobLogModalLink').href = '/ui/job?id=' + encodeURIComponent(jobId);
            document.getElementById('jobLogModal').style.display = 'block';

            const box = document.getElementById('jobLogModalBody');
            box.__logKeys = [];
            box.textContent = 'Loading log…';

            const live = document.getElementById('jobLogLive');
            stopJobLogTail = tailJobLog(box, jobId, {
                onUpdate: function(data) {
                    if (!live) { return; }
                    // The badge is the only sign that the view is following a
                    // job rather than showing a finished one.
                    live.style.display = data.running ? 'inline-flex' : 'none';
                },
                onStop: function() {
                    if (live) { live.style.display = 'none'; }
                },
            });
        }

        function stopJobLogTailIfRunning() {
            if (stopJobLogTail) {
                stopJobLogTail();
                stopJobLogTail = null;
            }
        }

        function closeJobLogModal() {
            stopJobLogTailIfRunning();
            jobLogModalJobId = null;
            document.getElementById('jobLogModal').style.display = 'none';
        }

        // Copies the log as plain text, which is the usual reason for selecting
        // it in the first place.
        async function copyJobLog() {
            const box = document.getElementById('jobLogModalBody');
            const lines = Array.from(box.querySelectorAll('.log-line')).map(function(line) {
                const ts = line.querySelector('.log-ts');
                const stage = line.querySelector('.log-stage');
                const msg = line.querySelector('.log-msg');
                return [ts ? ts.textContent : '', stage ? stage.textContent : '', msg ? msg.textContent : '']
                    .filter(Boolean).join(' ');
            });
            const text = lines.length ? lines.join('\n') : box.textContent;

            const status = document.getElementById('jobLogCopyStatus');
            try {
                await navigator.clipboard.writeText(text);
                status.textContent = 'Copied';
            } catch (e) {
                status.textContent = 'Copy failed — select the text manually';
            }
            setTimeout(function() { status.textContent = ''; }, 2000);
        }

        // Close on Escape, and on a click of the backdrop itself (but not of the
        // panel, so dragging a selection past the edge does not dismiss it).
        document.addEventListener('keydown', function(e) {
            if (e.key === 'Escape' && jobLogModalJobId !== null) closeJobLogModal();
        });

        // ---- Worker capability detection ----

        function apiUrlWithKey(path, params) {
            const qs = new URLSearchParams(params || {});
            const apiKey = localStorage.getItem(apiKeyStorageKey);
            if (apiKey) { qs.set('api_key', apiKey); }
            const q = qs.toString();
            return q ? path + '?' + q : path;
        }

        // Kept for the manual Refresh button; the live stream supplies the same
        // data every couple of seconds.
        async function loadCapabilities() {
            const tbody = document.getElementById('capabilities-tbody');
            if (!tbody) { return; }
            try {
                const res = await fetch(apiUrlWithKey('/api/workers'));
                if (!res.ok) { throw new Error('HTTP ' + res.status); }
                const data = await res.json();
                renderCapabilities((data.workers || []).map(function(w) {
                    const c = w.capabilities || {};
                    return {worker_id: w.worker_id, gpu: c.gpu, backends: c.backends || []};
                }));
            } catch (err) {
                tbody.textContent = '';
                const tr = document.createElement('tr');
                const td = document.createElement('td');
                td.colSpan = 7;
                td.textContent = 'Failed to load capabilities: ' + err.message;
                tr.appendChild(td);
                tbody.appendChild(tr);
            }
        }

        function renderCapabilities(workers) {
            const tbody = document.getElementById('capabilities-tbody');
            if (!tbody) { return; }
            tbody.textContent = '';

            let rows = 0;
            workers.forEach(function(wk) {
                const caps = wk.capabilities || wk;
                if (!caps || !caps.backends) { return; }

                caps.backends.forEach(function(b, idx) {
                    rows++;
                    const tr = document.createElement('tr');

                    // Every row repeats the worker. Leaving it blank on
                    // continuation rows made a single worker's backends look like
                    // several separate workers.
                    const tdWorker = document.createElement('td');
                    tdWorker.textContent = wk.worker_id;
                    if (idx > 0) { tdWorker.style.opacity = '.35'; }

                    const tdGpu = document.createElement('td');
                    tdGpu.textContent = caps.gpu || wk.gpu || '-';
                    if (idx > 0) { tdGpu.style.opacity = '.35'; }

                    const tdBackend = document.createElement('td');
                    tdBackend.textContent = b.name === 'none' ? 'CPU' : b.name;

                    const tdCodec = document.createElement('td');
                    tdCodec.textContent = b.codec || '-';

                    const tdEncoder = document.createElement('td');
                    tdEncoder.textContent = b.encoder || '-';

                    const tdResult = document.createElement('td');
                    const badge = document.createElement('span');
                    if (b.working) {
                        badge.className = 'badge badge-completed';
                        badge.textContent = 'working';
                    } else if (b.available) {
                        badge.className = 'badge badge-failed';
                        badge.textContent = 'unusable';
                    } else {
                        badge.className = 'badge badge-pending';
                        badge.textContent = 'not built in';
                    }
                    tdResult.appendChild(badge);

                    const tdDetail = document.createElement('td');
                    tdDetail.className = 'truncate';
                    tdDetail.title = b.detail || '';
                    tdDetail.textContent = b.detail || '';

                    tr.append(tdWorker, tdGpu, tdBackend, tdCodec, tdEncoder, tdResult, tdDetail);
                    tbody.appendChild(tr);
                });
            });

            if (rows === 0) {
                const tr = document.createElement('tr');
                const td = document.createElement('td');
                td.colSpan = 7;
                td.textContent = 'No worker has reported capabilities yet. They are probed when a worker starts.';
                tr.appendChild(td);
                tbody.appendChild(tr);
            }
        }

        // Mark hardware-acceleration options that no online worker can actually
        // run, so a backend that would fail every job cannot be selected blind.
        // Driven by the live stream, so a worker coming or going is reflected
        // without reloading the page.
        function applyBackendSupport(data) {
            if (!data || !data.known || !data.backends) { return; }

            ['hwaccel', 'lib-hwaccel'].forEach(function(id) {
                const sel = document.getElementById(id);
                if (!sel) { return; }
                Array.from(sel.options).forEach(function(opt) {
                    if (!opt.value || opt.value === '') { return; }
                    const info = data.backends[opt.value];
                    const base = opt.textContent.replace(/ — .*$/, '');
                    if (info && info.supported) {
                        opt.textContent = base;
                        opt.title = info.workers + ' worker(s) verified this backend';
                    } else {
                        opt.textContent = base + ' — not supported by any worker';
                        opt.title = (info && info.detail) ? info.detail : 'No online worker verified this backend';
                    }
                });
            });
        }

        // The Configuration form is not overwritten while it is on screen —
        // replacing fields under someone who is editing them would be hostile —
        // so a change made elsewhere is surfaced as a notice instead.
        let knownConfigVersion = null;
        function noteConfigVersion(version) {
            if (knownConfigVersion === null) {
                knownConfigVersion = version;
                return;
            }
            if (version === knownConfigVersion) { return; }

            knownConfigVersion = version;
            const banner = document.getElementById('config-changed-notice');
            if (banner) { banner.style.display = 'block'; }
        }

        // ---- Library management ----

        function libraryApiUrl(params) {
            let url = '/api/libraries';
            const apiKey = localStorage.getItem(apiKeyStorageKey);
            const qs = new URLSearchParams(params || {});
            if (apiKey) { qs.set('api_key', apiKey); }
            const q = qs.toString();
            return q ? url + '?' + q : url;
        }

        async function loadLibraries() {
            const tbody = document.getElementById('libraries-tbody');
            if (!tbody) { return; }
            try {
                const res = await fetch(libraryApiUrl());
                if (!res.ok) { throw new Error('HTTP ' + res.status); }
                const data = await res.json();
                renderLibraries(data.libraries || []);
            } catch (err) {
                tbody.textContent = '';
                const tr = document.createElement('tr');
                const td = document.createElement('td');
                td.colSpan = 7;
                td.textContent = 'Failed to load libraries: ' + err.message;
                tr.appendChild(td);
                tbody.appendChild(tr);
            }
        }

        // Rows are keyed and reused across refreshes. Wiping a tbody and
        // rebuilding it destroys whatever the user is interacting with: a
        // browser only fires click when mousedown and mouseup land on the
        // same element, so buttons silently did nothing whenever a refresh
        // landed mid-click. Reusing rows also preserves text selection.
        function syncRows(tbody, items, build, keyOf) {
            const getKey = keyOf || function(item) { return item.id; };

            // Drop any "nothing here" placeholder before syncing; it is
            // re-added below if the table really is empty.
            Array.from(tbody.querySelectorAll('tr[data-placeholder]')).forEach(function(tr) {
                tr.remove();
            });

            const existing = new Map();
            Array.from(tbody.children).forEach(function(tr) {
                const key = tr.getAttribute('data-key');
                if (key) { existing.set(key, tr); }
            });

            const seen = new Set();
            let previous = null;

            items.forEach(function(item) {
                const key = String(getKey(item));
                seen.add(key);

                let tr = existing.get(key);
                if (!tr) {
                    tr = document.createElement('tr');
                    tr.setAttribute('data-key', key);
                    build(tr, item, true);
                } else {
                    build(tr, item, false);
                }

                // Keep display order without detaching a row that is
                // already in the right place.
                const shouldFollow = previous ? previous.nextSibling : tbody.firstChild;
                if (tr !== shouldFollow) {
                    tbody.insertBefore(tr, shouldFollow);
                }
                previous = tr;
            });

            existing.forEach(function(tr, key) {
                if (!seen.has(key)) { tr.remove(); }
            });

            if (!tbody.children.length) {
                const tr = document.createElement('tr');
                tr.setAttribute('data-placeholder', '1');
                const td = document.createElement('td');
                td.colSpan = parseInt(tbody.getAttribute('data-cols'), 10) || 1;
                td.className = 'no-data';
                td.textContent = tbody.getAttribute('data-empty') || '';
                tr.appendChild(td);
                tbody.appendChild(tr);
            }
        }

        function renderLibraries(libs) {
            const tbody = document.getElementById('libraries-tbody');
            if (!tbody) { return; }

            syncRows(tbody, libs, function(tr, lib, isNew) {
                const outputText = lib.replace_source ? '(replaces source)' : (lib.output_path || '-');
                const eff = lib.effective || {};
                const summary = [eff.resolution, eff.codec, eff.bitrate, eff.output_format]
                    .filter(Boolean).join(' / ') || '-';
                const overrides = lib.conversion ? Object.keys(lib.conversion) : [];

                if (isNew) {
                    const tdName = document.createElement('td');
                    tdName.className = 'lib-name';
                    tdName.textContent = lib.name;

                    const tdPath = document.createElement('td');
                    tdPath.className = 'lib-path truncate';
                    tdPath.title = lib.path;
                    tdPath.textContent = lib.path;

                    const tdOut = document.createElement('td');
                    tdOut.className = 'lib-output truncate';
                    tdOut.title = outputText;
                    tdOut.textContent = outputText;

                    const tdSettings = document.createElement('td');
                    tdSettings.className = 'lib-settings';
                    renderLibrarySettings(tdSettings, summary, overrides);

                    const tdJobs = document.createElement('td');
                    tdJobs.className = 'lib-jobs';
                    tdJobs.textContent = lib.job_count;

                    const tdStatus = document.createElement('td');
                    const badge = document.createElement('span');
                    badge.className = 'badge ' + (lib.enabled ? 'badge-success' : 'badge-failed') + ' lib-status';
                    badge.textContent = lib.enabled ? 'Enabled' : 'Disabled';
                    tdStatus.appendChild(badge);

                    const tdActions = document.createElement('td');
                    tdActions.className = 'lib-actions';
                    buildLibraryActions(tdActions, lib);

                    tr.append(tdName, tdPath, tdOut, tdSettings, tdJobs, tdStatus, tdActions);
                    return;
                }

                // Refresh the values that move, leaving the buttons alone so a
                // click is never landing on a node about to be replaced.
                const setText = function(sel, value) {
                    const el = tr.querySelector(sel);
                    if (el && el.textContent !== value) { el.textContent = value; }
                };
                setText('.lib-name', lib.name);
                setText('.lib-path', lib.path);
                setText('.lib-output', outputText);
                setText('.lib-jobs', String(lib.job_count));

                const settingsCell = tr.querySelector('.lib-settings');
                if (settingsCell && settingsCell.getAttribute('data-summary') !== summary + '|' + overrides.join(',')) {
                    settingsCell.textContent = '';
                    renderLibrarySettings(settingsCell, summary, overrides);
                }

                const badge = tr.querySelector('.lib-status');
                const statusText = lib.enabled ? 'Enabled' : 'Disabled';
                if (badge && badge.textContent !== statusText) {
                    badge.textContent = statusText;
                    badge.className = 'badge ' + (lib.enabled ? 'badge-success' : 'badge-failed') + ' lib-status';

                    // Enable and Disable swap, so that cell is rebuilt on change.
                    const cell = tr.querySelector('.lib-actions');
                    if (cell) { cell.textContent = ''; buildLibraryActions(cell, lib); }
                }
            });
        }

        function renderLibrarySettings(cell, summary, overrides) {
            cell.setAttribute('data-summary', summary + '|' + overrides.join(','));
            cell.appendChild(document.createTextNode(summary));

            if (overrides.length) {
                cell.appendChild(document.createTextNode(' '));
                const badge = document.createElement('span');
                badge.className = 'badge badge-completed';
                badge.textContent = 'custom';
                badge.title = 'Overrides: ' + overrides.join(', ');
                cell.appendChild(badge);
            }
        }

        function buildLibraryActions(cell, lib) {
            const btnEdit = document.createElement('button');
            btnEdit.className = 'btn btn-sm';
            btnEdit.textContent = 'Edit';
            btnEdit.onclick = function() { editLibrary(lib); };

            const btnToggle = document.createElement('button');
            btnToggle.className = 'btn btn-sm';
            btnToggle.textContent = lib.enabled ? 'Disable' : 'Enable';
            btnToggle.onclick = function() {
                saveLibrary(Object.assign({}, lib, {enabled: !lib.enabled}));
            };

            const btnDel = document.createElement('button');
            btnDel.className = 'btn btn-danger btn-sm';
            btnDel.textContent = 'Remove';
            btnDel.onclick = function() { deleteLibrary(lib); };

            cell.append(btnEdit, document.createTextNode(' '), btnToggle, document.createTextNode(' '), btnDel);
        }

        function newLibrary() {
            document.getElementById('library-editor-title').textContent = 'Add Library';
            document.getElementById('lib-id').value = '';
            document.getElementById('lib-name').value = '';
            document.getElementById('lib-path').value = '';
            document.getElementById('lib-output').value = '';
            document.getElementById('lib-extensions').value = '';
            document.getElementById('lib-depth').value = '';
            document.getElementById('lib-enabled').checked = true;
            document.getElementById('lib-replace').checked = false;
            setLibraryConversion(null);
            document.getElementById('library-editor').style.display = 'block';
        }

        function editLibrary(lib) {
            document.getElementById('library-editor-title').textContent = 'Edit Library';
            document.getElementById('lib-id').value = lib.id;
            document.getElementById('lib-name').value = lib.name;
            document.getElementById('lib-path').value = lib.path;
            document.getElementById('lib-output').value = lib.output_path || '';
            document.getElementById('lib-extensions').value = (lib.video_extensions || []).join(', ');
            document.getElementById('lib-depth').value =
                (lib.recursive_depth === null || lib.recursive_depth === undefined) ? '' : lib.recursive_depth;
            document.getElementById('lib-enabled').checked = !!lib.enabled;
            document.getElementById('lib-replace').checked = !!lib.replace_source;
            setLibraryConversion(lib.conversion);
            document.getElementById('library-editor').style.display = 'block';
        }

        // Conversion override fields, keyed by the JSON field they map to.
        const libConversionFields = {
            resolution:    'lib-resolution',
            codec:         'lib-codec',
            bitrate:       'lib-bitrate',
            preset:        'lib-preset',
            output_format: 'lib-format',
            audio_codec:   'lib-audio-codec',
            audio_bitrate: 'lib-audio-bitrate',
            hwaccel:       'lib-hwaccel'
        };

        function setLibraryConversion(conv) {
            Object.keys(libConversionFields).forEach(function(key) {
                const el = document.getElementById(libConversionFields[key]);
                if (el) { el.value = (conv && conv[key]) ? conv[key] : ''; }
            });
        }

        // Collect only the fields that were actually set; omitting a field means
        // the library inherits the global value.
        function readLibraryConversion() {
            const conv = {};
            let any = false;
            Object.keys(libConversionFields).forEach(function(key) {
                const el = document.getElementById(libConversionFields[key]);
                const val = el ? el.value.trim() : '';
                if (val) { conv[key] = val; any = true; }
            });
            return any ? conv : null;
        }

        function cancelLibraryEdit() {
            document.getElementById('library-editor').style.display = 'none';
            document.getElementById('library-status').textContent = '';
        }

        async function saveLibrary(lib) {
            const status = document.getElementById('library-status');
            try {
                const res = await fetch(libraryApiUrl(), {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(lib)
                });
                if (!res.ok) {
                    const body = await res.json().catch(function() { return null; });
                    if (body && body.errors) {
                        throw new Error(body.errors.map(function(e) {
                            return e.field + ': ' + e.message;
                        }).join('; '));
                    }
                    throw new Error('HTTP ' + res.status);
                }
                status.textContent = 'Saved.';
                cancelLibraryEdit();
                // First paint before the stream's first tick arrives.
        loadLibraries();
        loadCapabilities();
            } catch (err) {
                status.textContent = 'Error: ' + err.message;
            }
        }

        async function deleteLibrary(lib) {
            if (!confirm('Remove library "' + lib.name + '"? Existing jobs are kept.')) { return; }
            try {
                const res = await fetch(libraryApiUrl({ id: lib.id }), { method: 'DELETE' });
                if (!res.ok) { throw new Error('HTTP ' + res.status); }
                loadLibraries();
            } catch (err) {
                alert('Failed to remove library: ' + err.message);
            }
        }

        document.getElementById('libraryForm').addEventListener('submit', function(e) {
            e.preventDefault();

            const extRaw = document.getElementById('lib-extensions').value.trim();
            const depthRaw = document.getElementById('lib-depth').value.trim();

            const lib = {
                id: document.getElementById('lib-id').value,
                name: document.getElementById('lib-name').value.trim(),
                path: document.getElementById('lib-path').value.trim(),
                output_path: document.getElementById('lib-output').value.trim(),
                enabled: document.getElementById('lib-enabled').checked,
                replace_source: document.getElementById('lib-replace').checked
            };

            if (extRaw) {
                lib.video_extensions = extRaw.split(',').map(function(s) {
                    s = s.trim().toLowerCase();
                    return s && s[0] !== '.' ? '.' + s : s;
                }).filter(Boolean);
            }
            if (depthRaw !== '') {
                lib.recursive_depth = parseInt(depthRaw, 10);
            }

            const conv = readLibraryConversion();
            if (conv) { lib.conversion = conv; }

            saveLibrary(lib);
        });

        loadLibraries();

        function connectStatsStream() {
            let url = '/api/stats/stream';
            const apiKey = localStorage.getItem(apiKeyStorageKey);
            if (apiKey) {
                url += '?api_key=' + encodeURIComponent(apiKey);
            }
            const evtSource = new EventSource(url);

            // The server emits a *named* SSE event ("event: stats"), which never
            // triggers onmessage — that only fires for unnamed/default events.
            evtSource.addEventListener('stats', function(event) {
                try {
                    const data = JSON.parse(event.data);

                    // Update stats
                    if (data.stats.pending !== undefined) document.getElementById('stat-pending').textContent = data.stats.pending;
                    if (data.stats.processing !== undefined) document.getElementById('stat-processing').textContent = data.stats.processing;
                    if (data.stats.completed !== undefined) document.getElementById('stat-completed').textContent = data.stats.completed;
                    if (data.stats.failed !== undefined) document.getElementById('stat-failed').textContent = data.stats.failed;
                    if (data.stats.workers !== undefined) document.getElementById('stat-workers').textContent = data.stats.workers;

                    // Update queue status
                    if (data.stats.queue_paused !== undefined) {
                        const isPaused = data.stats.queue_paused;
                        window.isQueuePaused = isPaused;
                        const badge = document.getElementById('queue-status-badge');
                        const btn = document.getElementById('toggle-queue-btn');
                        if (isPaused) {
                            badge.textContent = 'Queue Paused';
                            badge.className = 'badge badge-failed';
                            btn.textContent = 'Resume Queue';
                            btn.style.background = '#67c23a';
                        } else {
                            badge.textContent = 'Queue Active';
                            badge.className = 'badge badge-success';
                            btn.textContent = 'Pause Queue';
                            btn.style.background = '#e6a23c';
                        }
                    }

                    function createElement(tag, opts = {}) {
                        const el = document.createElement(tag);
                        if (opts.text) el.textContent = opts.text;
                        if (opts.className) el.className = opts.className;
                        if (opts.title) el.title = opts.title;
                        // Without this, every anchor built here was rendered with no
                        // href at all — a link that looked like one but could not be
                        // clicked or opened in a new tab.
                        if (opts.href) el.href = opts.href;
                        if (opts.id) el.id = opts.id;
                        if (opts.onclick) el.onclick = opts.onclick;
                        if (opts.innerHTML) el.innerHTML = opts.innerHTML; // only for static UI bits, not data
                        if (opts.style) {
                            for (let key in opts.style) {
                                el.style[key] = opts.style[key];
                            }
                        }
                        return el;
                    }

                    // Rows are keyed by job id and reused across refreshes. Wiping a
                    // tbody and rebuilding it every two seconds destroys whatever the
                    // user is interacting with: a browser only fires click when
                    // mousedown and mouseup land on the same element, so links and
                    // buttons in these tables silently did nothing whenever a refresh
                    // landed mid-click. Reusing rows also preserves text selection.
                    function buildWorkerActions(cell, worker) {
                        cell.setAttribute('data-state', String(worker.status));

                        const btnConfig = createElement('button', {text: '⚙️ Configure', className: 'btn btn-primary btn-sm'});
                        btnConfig.onclick = () => openWorkerSettings(worker.worker_id);
                        cell.appendChild(btnConfig);

                        if (worker.status !== 'offline') {
                            if (worker.status === 'paused') {
                                const btnResume = createElement('button', {text: '▶️ Resume', className: 'btn btn-success btn-sm', style: {marginLeft: '5px'}});
                                btnResume.onclick = () => resumeWorker(worker.worker_id);
                                cell.appendChild(btnResume);
                            } else {
                                const btnPause = createElement('button', {text: '⏸️ Pause', className: 'btn btn-warning btn-sm', style: {marginLeft: '5px'}});
                                btnPause.onclick = () => pauseWorker(worker.worker_id);
                                cell.appendChild(btnPause);
                            }
                        }

                        const btnRemove = createElement('button', {text: '🗑️ Remove', className: 'btn btn-danger btn-sm', style: {marginLeft: '5px'}});
                        btnRemove.onclick = () => removeWorker(worker.worker_id);
                        cell.appendChild(btnRemove);
                    }

                    function buildHistoryActions(cell, job) {
                        const btnLog = createElement('button', {text: 'Log', className: 'btn btn-secondary btn-sm'});
                        btnLog.onclick = () => openJobLogModal(job.id);
                        cell.appendChild(btnLog);
                        cell.appendChild(document.createTextNode(' '));

                        if (job.status === 'failed') {
                            const btnRetry = createElement('button', {text: 'Retry', className: 'btn btn-primary btn-sm'});
                            btnRetry.onclick = () => retryJob(job.id);
                            cell.appendChild(btnRetry);
                            cell.appendChild(document.createTextNode(' '));
                        }
                        if (job.status === 'completed' || job.status === 'failed' || job.status === 'cancelled') {
                            const btnRequeue = createElement('button', {text: 'Re-queue', className: 'btn btn-secondary btn-sm'});
                            btnRequeue.onclick = () => requeueJob(job.id);
                            cell.appendChild(btnRequeue);
                        }
                    }


                    // Update pending jobs table
                    if (data.pending_jobs && document.getElementById('pending-jobs')) {
                        syncRows(document.getElementById('pending-jobs'), data.pending_jobs, function(tr, job, isNew) {
                            const prio = job.priority !== undefined ? job.priority : 5;

                            if (isNew) {
                                const tdId = createElement('td');
                                const aId = createElement('a', {
                                    href: '/ui/job?id=' + job.id,
                                    className: 'job-link',
                                    title: 'Open job details'
                                });
                                aId.appendChild(createElement('code', {text: job.id}));
                                tdId.appendChild(aId);

                                const tdSource = createElement('td', {
                                    text: job.rel_path || job.source_path,
                                    className: 'truncate',
                                    title: job.source_path
                                });

                                const tdPrio = createElement('td');
                                tdPrio.appendChild(createElement('span', {text: prio, className: 'badge badge-pending prio-badge'}));

                                const tdCreated = createElement('td', {text: new Date(job.created_at).toLocaleString()});

                                const tdActions = createElement('td');
                                const btnPrio = createElement('button', {text: 'Priority', className: 'btn btn-primary btn-sm'});
                                btnPrio.onclick = () => updatePriority(job.id, prio);
                                const btnCancel = createElement('button', {text: 'Cancel', className: 'btn btn-danger btn-sm'});
                                btnCancel.onclick = () => cancelJob(job.id);
                                tdActions.append(btnPrio, document.createTextNode(' '), btnCancel);

                                tr.append(tdId, tdSource, tdPrio, tdCreated, tdActions);
                                return;
                            }

                            // Only the priority can change while a job waits.
                            const badge = tr.querySelector('.prio-badge');
                            if (badge && badge.textContent !== String(prio)) { badge.textContent = prio; }
                        });
                    }

                    // Update processing jobs table
                    if (data.processing_jobs && document.getElementById('processing-jobs')) {
                        syncRows(document.getElementById('processing-jobs'), data.processing_jobs, function(tr, job, isNew) {
                            const pct = job.progress || 0;

                            if (isNew) {
                                const tdId = createElement('td');
                                const aId = createElement('a', {
                                    href: '/ui/job?id=' + job.id,
                                    className: 'job-link',
                                    title: 'Open job details'
                                });
                                aId.appendChild(createElement('code', {text: job.id}));
                                tdId.appendChild(aId);

                                const tdLibrary = createElement('td');
                                tdLibrary.appendChild(createElement('span', {
                                    text: job.library_name || 'Unknown',
                                    className: 'badge badge-pending'
                                }));

                                const tdSource = createElement('td', {
                                    text: job.rel_path || job.source_path,
                                    className: 'truncate',
                                    title: job.source_path
                                });

                                const tdStage = createElement('td');
                                tdStage.appendChild(createElement('span', {text: job.stage || '—', className: 'stage-text'}));

                                const tdProg = createElement('td');
                                const progRow = createElement('div', {className: 'progress-row'});
                                const progBar = createElement('div', {className: 'progress-bar', style: {width: '100px'}});
                                progBar.appendChild(createElement('div', {className: 'progress-fill', style: {width: pct + '%'}}));
                                progRow.appendChild(progBar);
                                progRow.appendChild(createElement('span', {text: pct.toFixed(1) + '%', className: 'progress-pct'}));
                                tdProg.appendChild(progRow);

                                const tdFps = createElement('td', {
                                    text: job.fps ? job.fps.toFixed(1) : '—',
                                    className: 'fps-text'
                                });
                                const tdEta = createElement('td', {text: formatEta(job.eta_seconds), className: 'eta-text'});
                                const tdWorker = createElement('td', {text: job.worker_id || '', className: 'worker-text'});
                                const tdStarted = createElement('td', {
                                    text: job.started_at ? new Date(job.started_at).toLocaleTimeString() : ''
                                });

                                const tdActions = createElement('td');
                                const btnLog = createElement('button', {text: 'Log', className: 'btn btn-secondary btn-sm'});
                                btnLog.onclick = () => openJobLogModal(job.id);
                                const btnCancel = createElement('button', {text: 'Cancel', className: 'btn btn-danger btn-sm'});
                                btnCancel.onclick = () => cancelJob(job.id);
                                tdActions.append(btnLog, document.createTextNode(' '), btnCancel);

                                tr.append(tdId, tdLibrary, tdSource, tdStage, tdProg, tdFps, tdEta, tdWorker, tdStarted, tdActions);
                                return;
                            }

                            // Refresh only the values that move, leaving the link and
                            // buttons — the things being clicked — untouched.
                            const setText = function(sel, value) {
                                const el = tr.querySelector(sel);
                                if (el && el.textContent !== value) { el.textContent = value; }
                            };
                            setText('.stage-text', job.stage || '—');
                            setText('.progress-pct', pct.toFixed(1) + '%');
                            setText('.fps-text', job.fps ? job.fps.toFixed(1) : '—');
                            setText('.eta-text', formatEta(job.eta_seconds));
                            setText('.worker-text', job.worker_id || '');

                            const fill = tr.querySelector('.progress-fill');
                            if (fill) { fill.style.width = pct + '%'; }
                        });
                    }

                    // Update recent jobs table
                    if (data.recent_jobs && document.getElementById('recent-jobs')) {
                        syncRows(document.getElementById('recent-jobs'), data.recent_jobs, function(tr, job, isNew) {
                            let statusClass = 'badge-pending';
                            if (job.status === 'completed') statusClass = 'badge-completed';
                            else if (job.status === 'failed') statusClass = 'badge-failed';

                            const completedAt = job.completed_at ? new Date(job.completed_at).toLocaleString() : '-';

                            if (isNew) {
                                const tdId = createElement('td');
                                const aId = createElement('a', {
                                    href: '/ui/job?id=' + job.id,
                                    className: 'job-link',
                                    title: 'Open job details'
                                });
                                aId.appendChild(createElement('code', {text: job.id}));
                                tdId.appendChild(aId);

                                const tdLibrary = createElement('td');
                                tdLibrary.appendChild(createElement('span', {
                                    text: job.library_name || 'Unknown',
                                    className: 'badge badge-pending'
                                }));

                                const tdSource = createElement('td', {
                                    text: job.rel_path || job.source_path,
                                    className: 'truncate',
                                    title: job.source_path
                                });

                                const tdStatus = createElement('td');
                                tdStatus.appendChild(createElement('span', {
                                    text: job.status,
                                    className: 'badge ' + statusClass + ' status-badge'
                                }));

                                const tdWorker = createElement('td', {text: job.worker_id || '', className: 'worker-text'});
                                const tdDuration = createElement('td', {
                                    text: job.source_duration ? Math.round(job.source_duration) + 's' : '-'
                                });
                                const tdCompleted = createElement('td', {text: completedAt, className: 'completed-text'});

                                const tdActions = createElement('td', {className: 'actions-cell'});
                                buildHistoryActions(tdActions, job);

                                tr.append(tdId, tdLibrary, tdSource, tdStatus, tdWorker, tdDuration, tdCompleted, tdActions);
                                return;
                            }

                            const badge = tr.querySelector('.status-badge');
                            const statusChanged = badge && badge.textContent !== job.status;
                            if (statusChanged) {
                                badge.textContent = job.status;
                                badge.className = 'badge ' + statusClass + ' status-badge';

                                // Which actions apply depends on the status, so rebuild
                                // just that cell when it changes.
                                const cell = tr.querySelector('.actions-cell');
                                if (cell) { cell.textContent = ''; buildHistoryActions(cell, job); }
                            }

                            const completedCell = tr.querySelector('.completed-text');
                            if (completedCell && completedCell.textContent !== completedAt) {
                                completedCell.textContent = completedAt;
                            }

                            const workerCell = tr.querySelector('.worker-text');
                            if (workerCell && workerCell.textContent !== (job.worker_id || '')) {
                                workerCell.textContent = job.worker_id || '';
                            }
                        });
                    }

                    // Libraries, capabilities and the acceleration pickers used to
                    // be fetched only when the page loaded, so job counts and a
                    // worker reconnecting never showed until a manual reload.
                    if (data.libraries) {
                        renderLibraries(data.libraries);
                    }
                    if (data.capabilities) {
                        renderCapabilities(data.capabilities.workers || []);
                        applyBackendSupport(data.capabilities);
                    }
                    if (data.config_version !== undefined) {
                        noteConfigVersion(data.config_version);
                    }

                    // Update workers table
                    if (data.workers && document.getElementById('workers-list')) {
                        syncRows(document.getElementById('workers-list'), data.workers, function(tr, worker, isNew) {
                            const statusText = worker.is_online ? 'Online' : 'Offline';
                            const statusBadge = worker.is_online ? 'badge-online' : 'badge-offline';
                            const lastSeen = worker.last_seen ? new Date(worker.last_seen).toLocaleTimeString() : '-';

                            if (isNew) {
                                const tdId = createElement('td');
                                tdId.appendChild(createElement('code', {text: worker.worker_id}));

                                const tdHost = createElement('td', {text: worker.hostname || ''});
                                const tdGpu = createElement('td', {text: worker.gpu || '', className: 'gpu-text'});
                                const tdJobs = createElement('td', {text: worker.active_jobs || 0, className: 'jobs-text'});
                                const tdCpu = createElement('td', {
                                    text: (worker.cpu_usage || 0).toFixed(1) + '%', className: 'cpu-text'
                                });
                                const tdMem = createElement('td', {
                                    text: (worker.memory_usage || 0).toFixed(1) + '%', className: 'mem-text'
                                });

                                const tdStatus = createElement('td');
                                tdStatus.appendChild(createElement('span', {
                                    text: statusText, className: 'badge ' + statusBadge + ' wstatus-badge'
                                }));

                                const tdSeen = createElement('td', {text: lastSeen, className: 'seen-text'});

                                const tdActions = createElement('td', {className: 'wactions-cell'});
                                buildWorkerActions(tdActions, worker);

                                tr.append(tdId, tdHost, tdGpu, tdJobs, tdCpu, tdMem, tdStatus, tdSeen, tdActions);
                                return;
                            }

                            const setText = function(sel, value) {
                                const el = tr.querySelector(sel);
                                if (el && el.textContent !== value) { el.textContent = value; }
                            };
                            setText('.gpu-text', worker.gpu || '');
                            setText('.jobs-text', String(worker.active_jobs || 0));
                            setText('.cpu-text', (worker.cpu_usage || 0).toFixed(1) + '%');
                            setText('.mem-text', (worker.memory_usage || 0).toFixed(1) + '%');
                            setText('.seen-text', lastSeen);

                            const badge = tr.querySelector('.wstatus-badge');
                            if (badge && badge.textContent !== statusText) {
                                badge.textContent = statusText;
                                badge.className = 'badge ' + statusBadge + ' wstatus-badge';
                            }

                            // Pause and Resume swap depending on state, so that cell is
                            // rebuilt only when the state actually changes.
                            const cell = tr.querySelector('.wactions-cell');
                            if (cell && cell.getAttribute('data-state') !== String(worker.status)) {
                                cell.textContent = '';
                                buildWorkerActions(cell, worker);
                            }
                        }, function(worker) { return worker.worker_id; });
                    }
                } catch (err) {
                    console.error('Failed to parse SSE data:', err);
                }
            });

            evtSource.onerror = function() {
                const indicator = document.getElementById('live-indicator');
                if (indicator) {
                    indicator.textContent = 'Disconnected 🔴';
                }
            };

            evtSource.onopen = function() {
                const indicator = document.getElementById('live-indicator');
                if (indicator) {
                    indicator.textContent = 'Live 🟢';
                }
            };
        }

        connectStatsStream();

        // Queue status functions
        async function toggleQueue() {
            const isPaused = window.isQueuePaused || false;
            const endpoint = isPaused ? '/api/queue/resume' : '/api/queue/pause';
            const action = isPaused ? 'resume' : 'pause';

            if (!confirm('Are you sure you want to ' + action + ' the global job queue?')) return;

            try {
                const resp = await authorizedFetch(endpoint, { method: 'POST' });
                if (!resp.ok) {
                    const txt = await resp.text();
                    alert('Failed to ' + action + ' queue: ' + txt);
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        // Worker admin functions
        async function pauseWorker(workerId) {
            if (!confirm('Pause worker ' + workerId + '?')) return;
            try {
                const resp = await authorizedFetch('/api/worker/pause?worker_id=' + encodeURIComponent(workerId), { method: 'POST' });
                if (!resp.ok) {
                    const txt = await resp.text();
                    alert('Failed to pause worker: ' + txt);
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        async function resumeWorker(workerId) {
            if (!confirm('Resume worker ' + workerId + '?')) return;
            try {
                const resp = await authorizedFetch('/api/worker/resume?worker_id=' + encodeURIComponent(workerId), { method: 'POST' });
                if (!resp.ok) {
                    const txt = await resp.text();
                    alert('Failed to resume worker: ' + txt);
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        async function removeWorker(workerId) {
            if (!confirm('Remove worker ' + workerId + '?')) return;
            try {
                const resp = await authorizedFetch('/api/worker?worker_id=' + encodeURIComponent(workerId), { method: 'DELETE' });
                if (!resp.ok) {
                    const txt = await resp.text();
                    alert('Failed to remove worker: ' + txt);
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        // Worker settings modal functions
        async function openWorkerSettings(workerId) {
            document.getElementById('modalWorkerId').textContent = workerId;
            document.getElementById('wsWorkerId').value = workerId;
            document.getElementById('workerModal').style.display = 'block';
            
            try {
                const resp = await authorizedFetch('/api/worker/settings?worker_id=' + encodeURIComponent(workerId));
                if (resp.ok) {
                    const settings = await resp.json();
                    document.getElementById('wsConcurrency').value = settings.concurrency || 3;
                    document.getElementById('wsHeartbeat').value = settings.heartbeat_interval || 30;
                    document.getElementById('wsJobCheck').value = settings.job_check_interval || 5;
                    document.getElementById('wsJobTimeout').value = settings.job_timeout || 7200;
                    document.getElementById('wsUseVulkan').value = settings.use_vulkan ? 'true' : 'false';
                    document.getElementById('wsDownloadTimeout').value = settings.download_timeout || 1800;
                    document.getElementById('wsUploadTimeout').value = settings.upload_timeout || 1800;
                    document.getElementById('wsMaxCacheSize').value = settings.max_cache_size || 10737418240;
                    document.getElementById('wsLogLevel').value = settings.log_level || 'info';
                    document.getElementById('wsBandwidthLimit').value = settings.bandwidth_limit || 0;
                }
            } catch (err) {
                console.error('Failed to load worker settings:', err);
            }
        }

        function closeWorkerModal() {
            document.getElementById('workerModal').style.display = 'none';
        }

        document.getElementById('workerSettingsForm').addEventListener('submit', async function(e) {
            e.preventDefault();
            const workerId = document.getElementById('wsWorkerId').value;
            const status = document.getElementById('workerModalStatus');
            
            const settings = {
                worker_id: workerId,
                concurrency: parseInt(document.getElementById('wsConcurrency').value) || 3,
                heartbeat_interval: parseInt(document.getElementById('wsHeartbeat').value) || 30,
                job_check_interval: parseInt(document.getElementById('wsJobCheck').value) || 5,
                job_timeout: parseInt(document.getElementById('wsJobTimeout').value) || 7200,
                max_api_requests_per_min: 60,
                download_timeout: parseInt(document.getElementById('wsDownloadTimeout').value) || 1800,
                upload_timeout: parseInt(document.getElementById('wsUploadTimeout').value) || 1800,
                max_cache_size: parseInt(document.getElementById('wsMaxCacheSize').value) || 10737418240,
                cache_cleanup_age: 86400,
                bandwidth_limit: parseInt(document.getElementById('wsBandwidthLimit').value) || 0,
                enable_resume_download: true,
                use_vulkan: document.getElementById('wsUseVulkan').value === 'true',
                ffmpeg_timeout: 7200,
                log_level: document.getElementById('wsLogLevel').value,
                log_format: 'json'
            };
            
            try {
                const resp = await authorizedFetch('/api/worker/settings?worker_id=' + encodeURIComponent(workerId), {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(settings)
                });
                if (resp.ok) {
                    status.className = 'status status-success';
                    status.textContent = 'Worker settings saved! The worker will use these settings on next config refresh.';
                    status.style.display = 'block';
                    setTimeout(function() { closeWorkerModal(); }, 2000);
                } else {
                    const data = await resp.json();
                    status.className = 'status status-error';
                    status.textContent = 'Error: ' + (data.error || 'Failed to save settings');
                    status.style.display = 'block';
                }
            } catch (err) {
                status.className = 'status status-error';
                status.textContent = 'Network error: ' + err.message;
                status.style.display = 'block';
            }
        });

        async function resetWorkerSettings() {
            const workerId = document.getElementById('wsWorkerId').value;
            if (!confirm('Reset settings for ' + workerId + ' to defaults?')) return;
            
            try {
                const resp = await authorizedFetch('/api/worker/settings?worker_id=' + encodeURIComponent(workerId), {
                    method: 'DELETE'
                });
                if (resp.ok) {
                    const status = document.getElementById('workerModalStatus');
                    status.className = 'status status-success';
                    status.textContent = 'Worker settings reset to defaults!';
                    status.style.display = 'block';
                    setTimeout(function() { closeWorkerModal(); }, 2000);
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }
    </script>
</body>
</html>
`))

// WorkerInfo represents worker information for display
type WorkerInfo struct {
	WorkerID      string
	Hostname      string
	GPU           string
	ActiveJobs    int
	IsOnline      bool
	LastHeartbeat time.Time
	CPUUsage      float64
	MemoryUsage   float64
}

// DashboardStats holds statistics for the dashboard
type DashboardStats struct {
	Pending    int
	Processing int
	Completed  int
	Failed     int
	Workers    int
}

// JobView decorates a job with the library it came from, so the UI can show
// "Movies / action/film.mkv" instead of an unreadable absolute path. The
// embedded job keeps every existing field available to templates and JSON.
type JobView struct {
	*models.Job
	LibraryName string `json:"library_name"`
	RelPath     string `json:"rel_path"`
}

// unknownLibraryName labels jobs that cannot be attributed to a library, e.g.
// jobs created before libraries existed whose source tree is no longer
// configured.
const unknownLibraryName = "Unknown"

// toJobViews resolves each job's library once for the whole batch.
func (s *Server) toJobViews(jobs []*models.Job) []JobView {
	libs := s.configMgr.GetLibraries()

	byID := make(map[string]config.Library, len(libs))
	for _, lib := range libs {
		byID[lib.ID] = lib
	}

	views := make([]JobView, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, newJobView(job, byID, libs))
	}
	return views
}

// newJobView attributes a single job to a library. Jobs carrying a library ID
// resolve directly; older jobs predate the column and are matched by path
// prefix so history stays readable after an upgrade.
func newJobView(job *models.Job, byID map[string]config.Library, libs []config.Library) JobView {
	view := JobView{Job: job, LibraryName: unknownLibraryName, RelPath: filepath.Base(job.SourcePath)}

	lib, ok := byID[job.LibraryID]
	if !ok {
		if matched, found := libraryForPath(job.SourcePath, libs); found {
			lib, ok = matched, true
		}
	}

	if !ok {
		return view
	}

	view.LibraryName = lib.Name
	if rel, err := filepath.Rel(lib.Path, job.SourcePath); err == nil && !strings.HasPrefix(rel, "..") {
		view.RelPath = rel
	}
	return view
}

// libraryForPath finds the library whose root contains path. The longest match
// wins so that a nested library is preferred over its parent.
func libraryForPath(path string, libs []config.Library) (config.Library, bool) {
	var best config.Library
	found := false

	for _, lib := range libs {
		if lib.Path == "" {
			continue
		}
		rel, err := filepath.Rel(lib.Path, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if !found || len(lib.Path) > len(best.Path) {
			best, found = lib, true
		}
	}

	return best, found
}

// WebUIData holds data for rendering the web interface
type WebUIData struct {
	Config         *config.ActiveConfig
	Workers        []WorkerInfo
	Stats          DashboardStats
	PendingJobs    []*models.Job
	ProcessingJobs []JobView
	RecentJobs     []JobView
	MasterURL      string                     // URL for workers to connect
	WorkerDefaults *models.RemoteWorkerConfig // Worker default settings
	APIKeyRequired bool
}

// ServeWebUI serves the web interface at the root path
func (s *Server) ServeWebUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Only serve root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	cfg := s.configMgr.Get()

	// Get workers from database
	workers, err := s.db.GetWorkers()
	if err != nil {
		slog.Error("Failed to get workers", "error", err)
		workers = nil
	}

	// Convert to WorkerInfo with online status
	now := time.Now()
	workerInfos := make([]WorkerInfo, 0, len(workers))
	onlineWorkers := 0
	for _, wk := range workers {
		isOnline := now.Sub(wk.Timestamp) < workerOnlineThreshold
		if isOnline {
			onlineWorkers++
		}
		workerInfos = append(workerInfos, WorkerInfo{
			WorkerID:      wk.WorkerID,
			Hostname:      wk.Hostname,
			GPU:           wk.GPU,
			ActiveJobs:    wk.ActiveJobs,
			IsOnline:      isOnline,
			LastHeartbeat: wk.Timestamp,
			CPUUsage:      wk.CPUUsage,
			MemoryUsage:   wk.MemoryUsage,
		})
	}

	// Get job statistics
	stats := DashboardStats{Workers: onlineWorkers}
	jobStats, err := s.db.GetJobStats()
	if err == nil {
		if v, ok := jobStats["pending"].(int); ok {
			stats.Pending = v
		}
		if v, ok := jobStats["processing"].(int); ok {
			stats.Processing = v
		}
		if v, ok := jobStats["completed"].(int); ok {
			stats.Completed = v
		}
		if v, ok := jobStats["failed"].(int); ok {
			stats.Failed = v
		}
	}

	// Get pending jobs (limit 20)
	pendingJobs, _ := s.db.GetJobsByStatus("pending", 20)

	// Get processing jobs
	processingJobs, _ := s.db.GetJobsByStatus("processing", 20)

	// Get recent completed/failed jobs
	recentJobs := make([]*models.Job, 0)
	completed, err := s.db.GetJobsByStatus("completed", 25)
	if err == nil {
		recentJobs = append(recentJobs, completed...)
	}
	failed, err := s.db.GetJobsByStatus("failed", 25)
	if err == nil {
		recentJobs = append(recentJobs, failed...)
	}

	// Build master URL for display
	masterURL := s.getMasterURL()

	// Get worker defaults for display (uses shared helper from http.go)
	workerDefaults := s.buildRemoteWorkerConfig()

	data := WebUIData{
		Config:         cfg,
		Workers:        workerInfos,
		Stats:          stats,
		PendingJobs:    pendingJobs,
		ProcessingJobs: s.toJobViews(processingJobs),
		RecentJobs:     s.toJobViews(recentJobs),
		MasterURL:      masterURL,
		WorkerDefaults: workerDefaults,
		APIKeyRequired: s.apiKey != "",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = webUITemplate.Execute(w, data)
	if err != nil {
		slog.Error("Failed to render web UI template", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
}

// getMasterURL returns the master URL for worker connections
func (s *Server) getMasterURL() string {
	if s.masterCfg.Server.Host == "0.0.0.0" {
		return fmt.Sprintf("http://<server-ip>:%d", s.masterCfg.Server.Port)
	}
	return "http://" + net.JoinHostPort(s.masterCfg.Server.Host, strconv.Itoa(s.masterCfg.Server.Port))
}

// GetConfig returns the current configuration as JSON
func (s *Server) GetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	cfg := s.configMgr.Get()

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(cfg)
	if err != nil {
		slog.Error("Failed to encode config", "error", err)
		return
	}
}

// UpdateConfig updates the conversion configuration
func (s *Server) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	// Decode onto a copy of the current configuration so that a caller which
	// omits a section (libraries, scanner, notifications) leaves it untouched
	// instead of silently deleting it. Only the keys actually present in the
	// request body are changed.
	cfg := *s.configMgr.Get()
	err := json.NewDecoder(r.Body).Decode(&cfg)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	err = s.configMgr.Update(&cfg)
	if err != nil {
		var validationErrs *config.ValidationErrors
		if errors.As(err, &validationErrs) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			encErr := json.NewEncoder(w).Encode(validationErrs)
			if encErr != nil {
				slog.Error("Failed to encode validation errors", "error", encErr)
			}
			return
		}
		slog.Error("Failed to update config", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	slog.Info("Configuration updated", "version", s.configMgr.Get().Version)

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(s.configMgr.Get())
	if err != nil {
		slog.Error("Failed to encode updated config", "error", err)
		return
	}
}

// jobDetailsTemplate is the HTML template for the individual job details page
var jobDetailsTemplate = template.Must(template.New("jobDetails").Parse(`<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" lang="en">
<head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>DarkStream - Job Details</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #1a1a2e; color: #eee; line-height: 1.6; }
        .container { max-width: 1000px; margin: 0 auto; padding: 20px; }
        h1 { color: #00d9ff; margin-bottom: 10px; display: flex; align-items: center; justify-content: space-between; }
        h2 { color: #00d9ff; margin: 20px 0 15px 0; font-size: 1.2em; border-bottom: 1px solid #333; padding-bottom: 5px; }
        .card { background: #16213e; border-radius: 8px; padding: 20px; box-shadow: 0 2px 10px rgba(0,0,0,0.3); margin-bottom: 20px; }
        .grid { display: grid; grid-template-columns: 150px 1fr; gap: 10px; margin-bottom: 15px; }
        .label { color: #aaa; font-weight: bold; }
        .value { word-break: break-all; }
        .badge { padding: 4px 8px; border-radius: 4px; font-size: 0.85em; font-weight: bold; }
        .badge-pending { background: #333; color: #ddd; }
        .badge-completed { background: #1b4332; color: #95d5b2; }
        .badge-failed { background: #5c2323; color: #f8d7da; }
        .btn { padding: 8px 15px; border: none; border-radius: 4px; cursor: pointer; font-size: 0.9em; transition: all 0.2s; margin-right: 5px; text-decoration: none; display: inline-block; }
        .btn-primary { background: #00d9ff; color: #000; font-weight: bold; }
        .btn-primary:hover { background: #00b8d4; }
        .btn-secondary { background: #333; color: #fff; }
        .btn-secondary:hover { background: #444; }
        .btn-danger { background: #dc3545; color: #fff; }
        .btn-danger:hover { background: #c82333; }
        .progress-bar { background: #0f0f23; border-radius: 4px; height: 20px; overflow: hidden; margin-top: 10px; }
        a { color: #00d9ff; }
        .table code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
        .job-link { color: #00d9ff; text-decoration: none; border-bottom: 1px dotted #00d9ff; cursor: pointer; }
        .job-link:hover { color: #fff; border-bottom-color: #fff; }
        .progress-row { display: flex; align-items: center; gap: 8px; }
        .progress-pct { font-variant-numeric: tabular-nums; min-width: 42px; }
        .fps-text, .eta-text { font-variant-numeric: tabular-nums; white-space: nowrap; }
        .stage-text { text-transform: capitalize; }
        .live-badge { display: inline-flex; align-items: center; gap: 6px; background: #1b3a2a; border: 1px solid #2d6a4f; color: #95d5b2; border-radius: 10px; padding: 1px 9px; font-size: .75em; font-weight: 600; letter-spacing: .04em; text-transform: uppercase; margin-left: 10px; vertical-align: middle; }
        .live-dot { width: 7px; height: 7px; border-radius: 50%; background: #52b788; animation: live-pulse 1.6s ease-in-out infinite; }
        @keyframes live-pulse { 0%, 100% { opacity: 1; } 50% { opacity: .25; } }
        .log-view { background: #0f0f23; border-radius: 4px; padding: 12px; max-height: 460px; overflow: auto; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .82em; line-height: 1.5; }
        .log-line { display: flex; gap: 10px; white-space: pre-wrap; word-break: break-word; }
        .log-ts { color: #666; flex: 0 0 auto; }
        .log-stage { color: #7aa2f7; flex: 0 0 70px; }
        .log-msg { flex: 1 1 auto; }
        .log-info .log-msg { color: #ddd; }
        .log-warn .log-msg { color: #ffd479; }
        .log-error .log-msg { color: #ff9d9d; }
        .progress-fill { background: #00d9ff; height: 100%; transition: width 0.3s ease; }
        .error-box { background: #5c2323; color: #f8d7da; padding: 15px; border-radius: 4px; margin-top: 10px; white-space: pre-wrap; font-family: monospace; }
        code { background: #0f0f23; padding: 2px 5px; border-radius: 3px; font-family: monospace; color: #00d9ff; }
    </style>
</head>
<body>
    <div class="container">
        <h1>
            <span>Job Details</span>
            <a href="/" class="btn btn-secondary">← Back to Dashboard</a>
        </h1>

        <div class="card">
            <div style="display: flex; justify-content: space-between; align-items: flex-start;">
                <div>
                    <h2 style="margin-top: 0;">Status</h2>
                    <div class="grid">
                        <div class="label">Job ID</div>
                        <div class="value"><code>{{.ID}}</code></div>

                        <div class="label">Status</div>
                        <div class="value">
                            <span class="badge {{if eq .Status "completed"}}badge-completed{{else if eq .Status "failed"}}badge-failed{{else}}badge-pending{{end}}">
                                {{.Status}}
                            </span>
                        </div>

                        <div class="label">Worker ID</div>
                        <div class="value">{{if .WorkerID}}<code>{{.WorkerID}}</code>{{else}}-{{end}}</div>

                        <div class="label">Priority</div>
                        <div class="value">{{.Priority}}</div>

                        <div class="label">Retries</div>
                        <div class="value">{{.RetryCount}} / {{.MaxRetries}}</div>
                    </div>
                </div>
                <div style="text-align: right;" id="action-buttons">
                    <!-- JS will populate buttons -->
                </div>
            </div>

            {{if eq .Status "failed"}}
            <div class="error-box">
                <strong>Error:</strong><br/>
                {{.ErrorMessage}}
            </div>
            {{end}}

            {{if eq .Status "processing"}}
            <div style="margin-top: 20px;">
                <div style="display: flex; justify-content: space-between;">
                    <span class="label">Progress: <span id="progress-text">0%</span></span>
                    <span class="label">Stage: <span id="stage-text">Unknown</span></span>
                    <span class="label">FPS: <span id="fps-text">0</span></span>
                </div>
                <div class="progress-bar">
                    <div class="progress-fill" id="progress-fill" style="width: 0%;"></div>
                </div>
            </div>
            {{end}}
        </div>

        <div class="card">
            <h2>File Information</h2>
            <div class="grid">
                <div class="label">Source Path</div>
                <div class="value"><code>{{.SourcePath}}</code></div>

                <div class="label">Output Path</div>
                <div class="value"><code>{{.OutputPath}}</code></div>

                <div class="label">Source Checksum</div>
                <div class="value">{{if .SourceChecksum}}<code>{{.SourceChecksum}}</code>{{else}}-{{end}}</div>

                <div class="label">Output Checksum</div>
                <div class="value">{{if .OutputChecksum}}<code>{{.OutputChecksum}}</code>{{else}}-{{end}}</div>

                <div class="label">Output Size</div>
                <div class="value">{{if .OutputSize}}{{.OutputSize}} bytes{{else}}-{{end}}</div>
            </div>
        </div>

        <div class="card">
            <h2>Timestamps</h2>
            <div class="grid">
                <div class="label">Created At</div>
                <div class="value">{{.CreatedAt.Format "2006-01-02 15:04:05"}}</div>

                <div class="label">Started At</div>
                <div class="value">{{if .StartedAt}}{{.StartedAt.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</div>

                <div class="label">Completed At</div>
                <div class="value">{{if .CompletedAt}}{{.CompletedAt.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</div>

                <div class="label">Duration</div>
                <div class="value">
                    {{if and .StartedAt .CompletedAt}}
                        {{/* Go template trick to format duration */}}
                        {{.CompletedAt.Sub .StartedAt}}
                    {{else}}
                        -
                    {{end}}
                </div>
            </div>
        </div>

        {{if or .SourceWidth .SourceHeight .SourceVideoCodec}}
        <div class="card">
            <h2>Source Video Metadata</h2>
            <div class="grid">
                <div class="label">Resolution</div>
                <div class="value">{{.SourceWidth}}x{{.SourceHeight}}</div>

                <div class="label">Duration</div>
                <div class="value">{{printf "%.2f" .SourceDuration}}s</div>

                <div class="label">Video Codec</div>
                <div class="value">{{.SourceVideoCodec}}</div>

                <div class="label">Audio Codec</div>
                <div class="value">{{.SourceAudioCodec}}</div>

                <div class="label">Bitrate</div>
                <div class="value">{{.SourceBitrate}} bps</div>

                <div class="label">File Size</div>
                <div class="value">{{.SourceFileSize}} bytes</div>
            </div>
        </div>
        {{end}}

        <div class="card">
            <h2>Job Log
                <button class="btn btn-secondary" style="float:right;font-size:.8em;padding:4px 10px"
                        onclick="reloadJobLog()">Refresh</button>
                <span id="job-log-live" class="live-badge" style="display:none"><span class="live-dot"></span>live</span>
            </h2>
            <p style="color:#aaa;font-size:.85em;margin-bottom:10px">
                Recorded by the worker while running this job, including FFmpeg's own output.
                While the job is running the log follows it on its own.
            </p>
            <div id="job-log" class="log-view">Loading&hellip;</div>
        </div>

    </div>

    <script>
        const jobId = '{{.ID}}';
        const jobStatus = '{{.Status}}';

        function createElement(tag, options = {}) {
            const el = document.createElement(tag);
            if (options.id) el.id = options.id;
            if (options.className) el.className = options.className;
            if (options.text) el.textContent = options.text;
            if (options.href) el.href = options.href;
            if (options.onclick) el.onclick = options.onclick;
            if (options.title) el.title = options.title;
            return el;
        }

        async function doAction(action) {
            const apiKey = localStorage.getItem('darkstreamAdminApiKey') || '';
            let url = '/api/job/' + action + '?job_id=' + encodeURIComponent(jobId);
            if (apiKey) {
                url += '&api_key=' + encodeURIComponent(apiKey);
            }
            try {
                const response = await fetch(url, { method: 'POST' });
                if (!response.ok) {
                    const text = await response.text();
                    alert('Action failed: ' + text);
                } else {
                    location.reload();
                }
            } catch (err) {
                alert('Request failed: ' + err);
            }
        }

        // Setup action buttons
        const btnContainer = document.getElementById('action-buttons');
        if (jobStatus === 'failed') {
            const btnRetry = createElement('button', {text: 'Retry Job', className: 'btn btn-primary'});
            btnRetry.onclick = () => doAction('retry');
            btnContainer.appendChild(btnRetry);
        }
        if (jobStatus === 'completed' || jobStatus === 'failed' || jobStatus === 'cancelled') {
            const btnRequeue = createElement('button', {text: 'Re-queue Job', className: 'btn btn-secondary'});
            btnRequeue.onclick = () => doAction('requeue');
            btnContainer.appendChild(btnRequeue);
        }
        if (jobStatus === 'pending' || jobStatus === 'processing') {
            const btnCancel = createElement('button', {text: 'Cancel Job', className: 'btn btn-danger'});
            btnCancel.onclick = () => doAction('cancel');
            btnContainer.appendChild(btnCancel);
        }

` + jobLogTailScript + `
        // The log follows the job while it runs, so this page can be left open
        // to watch a conversion rather than reloaded to see what happened.
        let stopJobLogTail = null;

        function reloadJobLog() {
            if (stopJobLogTail) { stopJobLogTail(); }

            const container = document.getElementById('job-log');
            container.__logKeys = [];
            container.textContent = 'Loading…';

            stopJobLogTail = tailJobLog(container, jobId, {
                onUpdate: function(data) {
                    const live = document.getElementById('job-log-live');
                    if (live) { live.style.display = data.running ? 'inline-flex' : 'none'; }
                },
                onStop: function() {
                    const live = document.getElementById('job-log-live');
                    if (live) { live.style.display = 'none'; }
                },
            });
        }

        reloadJobLog();

        // Progress polling for processing jobs
        if (jobStatus === 'processing') {
            setInterval(async () => {
                let url = '/api/job/progress?job_id=' + encodeURIComponent(jobId);
                const apiKey = localStorage.getItem('darkstreamAdminApiKey');
                if (apiKey) url += '&api_key=' + encodeURIComponent(apiKey);

                try {
                    const res = await fetch(url);
                    if (res.ok) {
                        const data = await res.json();
                        const progress = data.progress || 0;
                        document.getElementById('progress-fill').style.width = progress + '%';
                        document.getElementById('progress-text').textContent = progress.toFixed(1) + '%';
                        document.getElementById('stage-text').textContent = data.stage || 'Unknown';
                        document.getElementById('fps-text').textContent = data.fps ? data.fps.toFixed(1) : '0';
                    } else if (res.status === 404) {
                        // Job might have finished, reload the page
                        location.reload();
                    }
                } catch (e) {
                    console.error('Progress fetch error:', e);
                }
            }, 2000);
        }
    </script>
</body>
</html>`))

func (s *Server) handleJobDetailsUI(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("id")
	if jobID == "" {
		http.Error(w, "Missing job ID", http.StatusBadRequest)
		return
	}

	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		slog.Error("Failed to get job for details UI", "error", err, "job_id", jobID)
		http.Error(w, "Job not found or error retrieving job", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = jobDetailsTemplate.Execute(w, job)
	if err != nil {
		slog.Error("Error rendering job details template", "error", err)
		// We can't send a 500 error properly here if headers are already sent,
		// but typically Execute writes headers only when it writes the body.
		return
	}
}
