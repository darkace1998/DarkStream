package server

import (
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"time"

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
    <title>DarkStream - Video Converter Configuration</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #1a1a2e; color: #eee; line-height: 1.6; }
        .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
        h1 { color: #00d9ff; margin-bottom: 10px; }
        h2 { color: #00d9ff; margin: 20px 0 15px 0; font-size: 1.3em; border-bottom: 1px solid #333; padding-bottom: 10px; }
        .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 30px; flex-wrap: wrap; gap: 10px; }
        .version { color: #888; font-size: 0.9em; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(350px, 1fr)); gap: 20px; }
        .card { background: #16213e; border-radius: 8px; padding: 20px; box-shadow: 0 2px 10px rgba(0,0,0,0.3); }
        .form-group { margin-bottom: 15px; }
        label { display: block; margin-bottom: 5px; color: #aaa; font-size: 0.9em; }
        input, select { width: 100%; padding: 10px 12px; border: 1px solid #333; border-radius: 4px; background: #0f0f23; color: #fff; font-size: 1em; }
        input:focus, select:focus { outline: none; border-color: #00d9ff; }
        select { cursor: pointer; }
        .btn { padding: 12px 24px; border: none; border-radius: 4px; cursor: pointer; font-size: 1em; transition: all 0.2s; }
        .btn-primary { background: #00d9ff; color: #000; font-weight: bold; }
        .btn-primary:hover { background: #00b8d4; }
        .btn-primary:disabled { background: #555; color: #888; cursor: not-allowed; }
        .status { padding: 10px 15px; border-radius: 4px; margin-bottom: 20px; }
        .status-success { background: #1b4332; color: #95d5b2; }
        .status-error { background: #5c2323; color: #f8d7da; }
        .workers-table { width: 100%; border-collapse: collapse; margin-top: 10px; }
        .workers-table th, .workers-table td { padding: 10px; text-align: left; border-bottom: 1px solid #333; }
        .workers-table th { color: #aaa; font-weight: normal; font-size: 0.9em; }
        .worker-status { padding: 3px 8px; border-radius: 3px; font-size: 0.85em; }
        .worker-online { background: #1b4332; color: #95d5b2; }
        .worker-offline { background: #5c2323; color: #f8d7da; }
        .meta { color: #666; font-size: 0.85em; margin-top: 15px; }
        .actions { margin-top: 20px; display: flex; gap: 10px; align-items: center; }
        .loading { display: none; }
        .loading.show { display: inline-block; margin-left: 10px; }
        @keyframes spin { to { transform: rotate(360deg); } }
        .spinner { width: 20px; height: 20px; border: 2px solid #333; border-top-color: #00d9ff; border-radius: 50%; animation: spin 0.8s linear infinite; display: inline-block; }
        .no-workers { color: #888; font-style: italic; padding: 20px; text-align: center; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div>
                <h1>DarkStream</h1>
                <span class="version">Video Converter Configuration</span>
            </div>
            <div class="meta">
                Version: {{.Config.Version}} | Last Updated: {{.Config.UpdatedAt.Format "2006-01-02 15:04:05"}}
            </div>
        </div>

        <div id="status" class="status" style="display: none;"></div>

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
                            <option value="placebo" {{if eq .Config.Video.Preset "placebo"}}selected{{end}}>Placebo</option>
                        </select>
                    </div>
                </div>

                <div class="card">
                    <h2>Audio Settings</h2>
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

                    <h2>Output Format</h2>
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

            <div class="actions">
                <button type="submit" class="btn btn-primary" id="saveBtn">Save Configuration</button>
                <div class="loading" id="loading"><span class="spinner"></span></div>
            </div>
        </form>

        <div class="card" style="margin-top: 30px;">
            <h2>Connected Workers</h2>
            {{if .Workers}}
            <table class="workers-table">
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
                        <td>{{.WorkerID}}</td>
                        <td>{{.Hostname}}</td>
                        <td>{{.GPU}}</td>
                        <td>{{.ActiveJobs}}</td>
                        <td><span class="worker-status {{if .IsOnline}}worker-online{{else}}worker-offline{{end}}">{{if .IsOnline}}Online{{else}}Offline{{end}}</span></td>
                        <td>{{.LastHeartbeat.Format "15:04:05"}}</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
            {{else}}
            <div class="no-workers">No workers connected</div>
            {{end}}
        </div>
    </div>

    <script>
        document.getElementById('configForm').addEventListener('submit', async function(e) {
            e.preventDefault();
            
            const btn = document.getElementById('saveBtn');
            const loading = document.getElementById('loading');
            const status = document.getElementById('status');
            
            btn.disabled = true;
            loading.classList.add('show');
            status.style.display = 'none';
            
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
                output: {
                    format: document.getElementById('format').value
                }
            };
            
            try {
                const resp = await fetch('/api/config', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(config)
                });
                
                const data = await resp.json();
                
                if (resp.ok) {
                    status.className = 'status status-success';
                    status.textContent = 'Configuration saved successfully! Version: ' + data.version;
                } else {
                    status.className = 'status status-error';
                    if (data.errors) {
                        status.textContent = 'Validation errors: ' + data.errors.map(function(e) { return e.field + ': ' + e.message; }).join(', ');
                    } else {
                        status.textContent = 'Error: ' + (data.error || 'Unknown error');
                    }
                }
            } catch (err) {
                status.className = 'status status-error';
                status.textContent = 'Network error: ' + err.message;
            }
            
            status.style.display = 'block';
            btn.disabled = false;
            loading.classList.remove('show');
        });
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

// WebUIData holds data for rendering the web interface
type WebUIData struct {
	Config  *config.ActiveConfig
	Workers []WorkerInfo
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
		// Continue without workers rather than failing
		workers = nil
	}

	// Convert to WorkerInfo with online status
	now := time.Now()
	workerInfos := make([]WorkerInfo, 0, len(workers))
	for _, wk := range workers {
		workerInfos = append(workerInfos, WorkerInfo{
			WorkerID:      wk.WorkerID,
			Hostname:      wk.Hostname,
			GPU:           wk.GPU,
			ActiveJobs:    wk.ActiveJobs,
			IsOnline:      now.Sub(wk.Timestamp) < workerOnlineThreshold,
			LastHeartbeat: wk.Timestamp,
		})
	}

	data := WebUIData{
		Config:  cfg,
		Workers: workerInfos,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webUITemplate.Execute(w, data); err != nil {
		slog.Error("Failed to render web UI template", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
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
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.configMgr.Update(&cfg); err != nil {
		var validationErrs *config.ValidationErrors
		if errors.As(err, &validationErrs) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			if encErr := json.NewEncoder(w).Encode(validationErrs); encErr != nil {
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
	if err := json.NewEncoder(w).Encode(s.configMgr.Get()); err != nil {
		slog.Error("Failed to encode updated config", "error", err)
		return
	}
}
