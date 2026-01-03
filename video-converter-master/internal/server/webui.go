package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
)

// workerOnlineThreshold defines how long since last heartbeat before a worker is considered offline
const workerOnlineThreshold = 2 * time.Minute

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
        .meta { color: #666; font-size: 0.85em; }
        .tabs { display: flex; border-bottom: 2px solid #333; margin-bottom: 20px; }
        .tab { padding: 12px 24px; cursor: pointer; color: #888; border-bottom: 2px solid transparent; margin-bottom: -2px; }
        .tab:hover { color: #00d9ff; }
        .tab.active { color: #00d9ff; border-bottom-color: #00d9ff; }
        .tab-content { display: none; }
        .tab-content.active { display: block; }
        .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 15px; margin-bottom: 20px; }
        .stat-card { background: #16213e; border-radius: 8px; padding: 15px; text-align: center; }
        .stat-value { font-size: 2em; color: #00d9ff; font-weight: bold; }
        .stat-label { color: #888; font-size: 0.85em; }
        .no-data { color: #888; font-style: italic; padding: 30px; text-align: center; }
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
            <div class="meta">
                Config Version: {{.Config.Version}} | Auto-refresh: <span id="countdown">30</span>s
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

        <!-- Tab Navigation -->
        <div class="tabs">
            <div class="tab active" data-tab="queue">📋 Job Queue</div>
            <div class="tab" data-tab="processing">⚙️ Processing</div>
            <div class="tab" data-tab="history">📜 History</div>
            <div class="tab" data-tab="workers">👥 Workers</div>
            <div class="tab" data-tab="config">⚙️ Configuration</div>
        </div>

        <!-- Queue Tab -->
        <div id="queue" class="tab-content active">
            <div class="card">
                <h2>Pending Jobs <button class="btn btn-danger btn-sm refresh-btn" onclick="cancelAllPending()">Cancel All Pending</button></h2>
                {{if .PendingJobs}}
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
                    <tbody id="pending-jobs">
                        {{range .PendingJobs}}
                        <tr>
                            <td><code>{{.ID}}</code></td>
                            <td class="truncate" title="{{.SourcePath}}">{{.SourcePath}}</td>
                            <td><span class="badge badge-pending">{{.Priority}}</span></td>
                            <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
                            <td><button class="btn btn-danger btn-sm" onclick="cancelJob('{{.ID}}')">Cancel</button></td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
                {{else}}
                <div class="no-data">No pending jobs in queue</div>
                {{end}}
            </div>
        </div>

        <!-- Processing Tab -->
        <div id="processing" class="tab-content">
            <div class="card">
                <h2>Currently Processing</h2>
                {{if .ProcessingJobs}}
                <table class="table">
                    <thead>
                        <tr>
                            <th>Job ID</th>
                            <th>Source</th>
                            <th>Worker</th>
                            <th>Progress</th>
                            <th>Started</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody id="processing-jobs">
                        {{range .ProcessingJobs}}
                        <tr>
                            <td><code>{{.ID}}</code></td>
                            <td class="truncate" title="{{.SourcePath}}">{{.SourcePath}}</td>
                            <td>{{.WorkerID}}</td>
                            <td>
                                <div class="progress-bar" style="width: 100px;">
                                    <div class="progress-fill" style="width: 0%;" id="progress-{{.ID}}"></div>
                                </div>
                            </td>
                            <td>{{if .StartedAt}}{{.StartedAt.Format "15:04:05"}}{{end}}</td>
                            <td><button class="btn btn-danger btn-sm" onclick="cancelJob('{{.ID}}')">Cancel</button></td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
                {{else}}
                <div class="no-data">No jobs currently processing</div>
                {{end}}
            </div>
        </div>

        <!-- History Tab -->
        <div id="history" class="tab-content">
            <div class="card">
                <h2>Recent Jobs (Last 50)</h2>
                {{if .RecentJobs}}
                <table class="table">
                    <thead>
                        <tr>
                            <th>Job ID</th>
                            <th>Source</th>
                            <th>Status</th>
                            <th>Worker</th>
                            <th>Duration</th>
                            <th>Completed</th>
                        </tr>
                    </thead>
                    <tbody>
                        {{range .RecentJobs}}
                        <tr>
                            <td><code>{{.ID}}</code></td>
                            <td class="truncate" title="{{.SourcePath}}">{{.SourcePath}}</td>
                            <td><span class="badge {{if eq .Status "completed"}}badge-completed{{else if eq .Status "failed"}}badge-failed{{else}}badge-pending{{end}}">{{.Status}}</span></td>
                            <td>{{.WorkerID}}</td>
                            <td>{{if .SourceDuration}}{{printf "%.0f" .SourceDuration}}s{{else}}-{{end}}</td>
                            <td>{{if .CompletedAt}}{{.CompletedAt.Format "2006-01-02 15:04"}}{{else}}-{{end}}</td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
                {{else}}
                <div class="no-data">No recent jobs</div>
                {{end}}
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
                <p style="color: #888; font-size: 0.85em;">Workers will automatically receive all configuration settings from this master.</p>
            </div>
            <div class="card" style="margin-top: 20px;">
                <h2>👥 Connected Workers</h2>
                {{if .Workers}}
                <table class="table">
                    <thead>
                        <tr>
                            <th>Worker ID</th>
                            <th>Hostname</th>
                            <th>GPU</th>
                            <th>Active Jobs</th>
                            <th>Status</th>
                            <th>Last Seen</th>
                        </tr>
                    </thead>
                    <tbody>
                        {{range .Workers}}
                        <tr>
                            <td><code>{{.WorkerID}}</code></td>
                            <td>{{.Hostname}}</td>
                            <td>{{.GPU}}</td>
                            <td>{{.ActiveJobs}}</td>
                            <td><span class="badge {{if .IsOnline}}badge-online{{else}}badge-offline{{end}}">{{if .IsOnline}}Online{{else}}Offline{{end}}</span></td>
                            <td>{{.LastHeartbeat.Format "15:04:05"}}</td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
                {{else}}
                <div class="no-data">No workers connected</div>
                {{end}}
            </div>
            <div class="card" style="margin-top: 20px;">
                <h2>⚙️ Worker Default Settings</h2>
                <p style="color: #888; margin-bottom: 15px; font-size: 0.9em;">These settings are provided to workers when they connect. Configure them in the master's config.yaml file under <code>worker_defaults:</code></p>
                <div class="grid">
                    <div>
                        <table class="table">
                            <tr><td style="color: #aaa;">Concurrency</td><td>{{.WorkerDefaults.Concurrency}} jobs</td></tr>
                            <tr><td style="color: #aaa;">Heartbeat Interval</td><td>{{.WorkerDefaults.HeartbeatInterval}}s</td></tr>
                            <tr><td style="color: #aaa;">Job Check Interval</td><td>{{.WorkerDefaults.JobCheckInterval}}s</td></tr>
                            <tr><td style="color: #aaa;">Job Timeout</td><td>{{.WorkerDefaults.JobTimeout}}s</td></tr>
                            <tr><td style="color: #aaa;">Use Vulkan GPU</td><td>{{if .WorkerDefaults.UseVulkan}}Yes{{else}}No{{end}}</td></tr>
                        </table>
                    </div>
                    <div>
                        <table class="table">
                            <tr><td style="color: #aaa;">Download Timeout</td><td>{{.WorkerDefaults.DownloadTimeout}}s</td></tr>
                            <tr><td style="color: #aaa;">Upload Timeout</td><td>{{.WorkerDefaults.UploadTimeout}}s</td></tr>
                            <tr><td style="color: #aaa;">Max Cache Size</td><td>{{.WorkerDefaults.MaxCacheSize}} bytes</td></tr>
                            <tr><td style="color: #aaa;">Log Level</td><td>{{.WorkerDefaults.LogLevel}}</td></tr>
                            <tr><td style="color: #aaa;">Resume Download</td><td>{{if .WorkerDefaults.EnableResumeDownload}}Enabled{{else}}Disabled{{end}}</td></tr>
                        </table>
                    </div>
                </div>
            </div>
        </div>

        <!-- Configuration Tab -->
        <div id="config" class="tab-content">
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
                <div style="margin-top: 20px;">
                    <button type="submit" class="btn btn-primary" id="saveBtn">💾 Save Configuration</button>
                    <div class="loading" id="loading"><span class="spinner"></span></div>
                </div>
            </form>
        </div>
    </div>

    <script>
        // Tab switching
        document.querySelectorAll('.tab').forEach(function(tab) {
            tab.addEventListener('click', function() {
                document.querySelectorAll('.tab').forEach(function(t) { t.classList.remove('active'); });
                document.querySelectorAll('.tab-content').forEach(function(c) { c.classList.remove('active'); });
                this.classList.add('active');
                document.getElementById(this.dataset.tab).classList.add('active');
            });
        });

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
                    preset: document.getElementById('preset').value
                },
                audio: {
                    codec: document.getElementById('audioCodec').value,
                    bitrate: document.getElementById('audioBitrate').value
                },
                output: { format: document.getElementById('format').value }
            };
            
            try {
                const resp = await fetch('/api/config', {
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

        // Cancel single job
        async function cancelJob(jobId) {
            if (!confirm('Cancel job ' + jobId + '?')) return;
            try {
                const resp = await fetch('/api/job/cancel?job_id=' + jobId, { method: 'POST' });
                if (resp.ok) {
                    location.reload();
                } else {
                    alert('Failed to cancel job');
                }
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        // Cancel all pending
        async function cancelAllPending() {
            if (!confirm('Cancel ALL pending jobs?')) return;
            try {
                const resp = await fetch('/api/jobs/cancel?status=pending', { method: 'POST' });
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

        // Auto-refresh countdown
        let countdown = 30;
        setInterval(function() {
            countdown--;
            document.getElementById('countdown').textContent = countdown;
            if (countdown <= 0) {
                location.reload();
            }
        }, 1000);

        // Fetch stats periodically
        async function updateStats() {
            try {
                const resp = await fetch('/api/stats');
                if (resp.ok) {
                    const data = await resp.json();
                    if (data.pending !== undefined) document.getElementById('stat-pending').textContent = data.pending;
                    if (data.processing !== undefined) document.getElementById('stat-processing').textContent = data.processing;
                    if (data.completed !== undefined) document.getElementById('stat-completed').textContent = data.completed;
                    if (data.failed !== undefined) document.getElementById('stat-failed').textContent = data.failed;
                }
            } catch (err) {
                console.error('Failed to update stats:', err);
            }
        }
        setInterval(updateStats, 5000);
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
}

// DashboardStats holds statistics for the dashboard
type DashboardStats struct {
	Pending    int
	Processing int
	Completed  int
	Failed     int
	Workers    int
}

// WebUIData holds data for rendering the web interface
type WebUIData struct {
	Config         *config.ActiveConfig
	Workers        []WorkerInfo
	Stats          DashboardStats
	PendingJobs    []*models.Job
	ProcessingJobs []*models.Job
	RecentJobs     []*models.Job
	MasterURL      string                      // URL for workers to connect
	WorkerDefaults *models.RemoteWorkerConfig  // Worker default settings
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
	masterURL := fmt.Sprintf("http://%s:%d", s.masterCfg.Server.Host, s.masterCfg.Server.Port)
	if s.masterCfg.Server.Host == "0.0.0.0" {
		masterURL = fmt.Sprintf("http://<server-ip>:%d", s.masterCfg.Server.Port)
	}

	// Get worker defaults for display
	workerDefaults := s.buildWorkerDefaults()

	data := WebUIData{
		Config:         cfg,
		Workers:        workerInfos,
		Stats:          stats,
		PendingJobs:    pendingJobs,
		ProcessingJobs: processingJobs,
		RecentJobs:     recentJobs,
		MasterURL:      masterURL,
		WorkerDefaults: workerDefaults,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = webUITemplate.Execute(w, data)
	if err != nil {
		slog.Error("Failed to render web UI template", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
}

// buildWorkerDefaults constructs the worker defaults from master config
func (s *Server) buildWorkerDefaults() *models.RemoteWorkerConfig {
	defaults := s.masterCfg.WorkerDefaults

	// Apply sensible defaults if not configured
	concurrency := defaults.Concurrency
	if concurrency <= 0 {
		concurrency = 3
	}

	heartbeatInterval := defaults.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = 30 * time.Second
	}

	jobCheckInterval := defaults.JobCheckInterval
	if jobCheckInterval <= 0 {
		jobCheckInterval = 5 * time.Second
	}

	jobTimeout := defaults.JobTimeout
	if jobTimeout <= 0 {
		jobTimeout = 2 * time.Hour
	}

	maxAPIRequestsPerMin := defaults.MaxAPIRequestsPerMin
	if maxAPIRequestsPerMin <= 0 {
		maxAPIRequestsPerMin = 60
	}

	downloadTimeout := defaults.DownloadTimeout
	if downloadTimeout <= 0 {
		downloadTimeout = 30 * time.Minute
	}

	uploadTimeout := defaults.UploadTimeout
	if uploadTimeout <= 0 {
		uploadTimeout = 30 * time.Minute
	}

	maxCacheSize := defaults.MaxCacheSize
	if maxCacheSize <= 0 {
		maxCacheSize = 10 * 1024 * 1024 * 1024 // 10GB
	}

	cacheCleanupAge := defaults.CacheCleanupAge
	if cacheCleanupAge <= 0 {
		cacheCleanupAge = 24 * time.Hour
	}

	ffmpegTimeout := defaults.FFmpegTimeout
	if ffmpegTimeout <= 0 {
		ffmpegTimeout = 2 * time.Hour
	}

	logLevel := defaults.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}

	logFormat := defaults.LogFormat
	if logFormat == "" {
		logFormat = "json"
	}

	conversionSettings := s.configMgr.GetConversionSettings()

	return &models.RemoteWorkerConfig{
		Concurrency:          concurrency,
		HeartbeatInterval:    int64(heartbeatInterval.Seconds()),
		JobCheckInterval:     int64(jobCheckInterval.Seconds()),
		JobTimeout:           int64(jobTimeout.Seconds()),
		MaxAPIRequestsPerMin: maxAPIRequestsPerMin,
		DownloadTimeout:      int64(downloadTimeout.Seconds()),
		UploadTimeout:        int64(uploadTimeout.Seconds()),
		MaxCacheSize:         maxCacheSize,
		CacheCleanupAge:      int64(cacheCleanupAge.Seconds()),
		BandwidthLimit:       defaults.BandwidthLimit,
		EnableResumeDownload: defaults.EnableResumeDownload,
		UseVulkan:            defaults.UseVulkan,
		FFmpegTimeout:        int64(ffmpegTimeout.Seconds()),
		Conversion:           *conversionSettings,
		LogLevel:             logLevel,
		LogFormat:            logFormat,
	}
}

// GetConfig returns the current configuration as JSON
func (s *Server) GetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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

	var cfg config.ActiveConfig
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
