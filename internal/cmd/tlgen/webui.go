package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/gen"
	"github.com/amarnathcjd/gogram/internal/cmd/tlgen/tlparser"
)

var (
	logBuffer        []string
	logMutex         sync.Mutex
	logBroadcast     = make(chan string, 100)
	webUITypeHandler *gen.WebUIMissingTypeHandler
)

type MissingTypeRequest struct {
	Type string `json:"type"`
}

type MissingTypeResponse struct {
	Input string `json:"input"`
}

type SchemaDiff struct {
	NewTypes       []string `json:"new_types"`
	DeletedTypes   []string `json:"deleted_types"`
	UpdatedTypes   []string `json:"updated_types"`
	NewMethods     []string `json:"new_methods"`
	DeletedMethods []string `json:"deleted_methods"`
	UpdatedMethods []string `json:"updated_methods"`
	LocalLayer     string   `json:"local_layer"`
	RemoteLayer    string   `json:"remote_layer"`
}

type GenerateRequest struct {
	Force     bool   `json:"force"`
	Docs      bool   `json:"docs"`
	Local     bool   `json:"local"`
	RemoteURL string `json:"remote_url"`
}

// CustomLogWriter intercepts log output and sends to web UI
type CustomLogWriter struct {
	original io.Writer
}

func (w *CustomLogWriter) Write(p []byte) (n int, err error) {
	msg := string(p)

	logMutex.Lock()
	logBuffer = append(logBuffer, msg)
	if len(logBuffer) > 1000 {
		logBuffer = logBuffer[len(logBuffer)-1000:]
	}
	logMutex.Unlock()

	select {
	case logBroadcast <- msg:
	default:
	}

	return w.original.Write(p)
}

func startWebUI() {
	// Set up custom log writer
	customWriter := &CustomLogWriter{original: os.Stderr}
	log.SetOutput(customWriter)

	// Initialize web UI type handler
	webUITypeHandler = &gen.WebUIMissingTypeHandler{
		RequestCh:  make(chan string, 10),
		ResponseCh: make(chan string, 10),
	}
	gen.SetMissingTypeHandler(webUITypeHandler)

	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/api/diff", handleDiff)
	http.HandleFunc("/api/generate", handleGenerate)
	http.HandleFunc("/api/fetch-remote", handleFetchRemote)
	http.HandleFunc("/api/logs", handleLogs)
	http.HandleFunc("/api/missing-type", handleMissingType)
	http.HandleFunc("/api/missing-type/response", handleMissingTypeResponse)

	port := ":8080"
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}
	log.Printf("INFO: Starting web UI on http://localhost%s\n", port)
	fmt.Printf("Open http://localhost%s in your browser\n", port)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("ERROR: Failed to start web server: %v\n", err)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

func handleDiff(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req struct {
		RemoteURL string `json:"remote_url"`
	}
	if r.Method == "POST" {
		json.NewDecoder(r.Body).Decode(&req)
	}

	localLayer := getAPILayerFromFile(tlLOC)

	// Get remote schema
	var remoteAPIVersion []byte
	var remoteLayer string
	var err error

	if req.RemoteURL != "" {
		remoteAPIVersion, remoteLayer, err = fetchFromSource(req.RemoteURL)
	} else {
		remoteAPIVersion, remoteLayer, err = getSourceLAYER(localLayer, true)
	}

	if err != nil {
		// Even if there's an error, try to return partial info
		json.NewEncoder(w).Encode(SchemaDiff{
			LocalLayer:     localLayer,
			RemoteLayer:    "error",
			NewTypes:       []string{},
			DeletedTypes:   []string{},
			UpdatedTypes:   []string{},
			NewMethods:     []string{},
			DeletedMethods: []string{},
			UpdatedMethods: []string{},
		})
		return
	}

	// Parse both schemas
	localSchemaBytes, _ := os.ReadFile(tlLOC)
	localSchema, _ := tlparser.ParseSchema(string(localSchemaBytes))

	remoteAPIVersion = cleanComments(remoteAPIVersion)
	remoteSchema, _ := tlparser.ParseSchema(string(remoteAPIVersion))

	diff := comparSchemas(localSchema, remoteSchema)
	diff.LocalLayer = localLayer
	diff.RemoteLayer = remoteLayer

	json.NewEncoder(w).Encode(diff)
}

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	go func() {
		if req.Local {
			log.Println("INFO: Starting generation from local schema file")
			if err := root(tlLOC, desLOC, req.Docs, getAPILayerFromFile(tlLOC)); err != nil {
				log.Printf("ERROR: Generation failed: %s\n", err)
			} else {
				log.Println("INFO: Generation completed successfully")
			}
		} else {
			localLayer := getAPILayerFromFile(tlLOC)
			var remoteAPIVersion []byte
			var remoteLayer string
			var err error

			if req.RemoteURL != "" {
				log.Printf("INFO: Fetching from custom URL: %s\n", req.RemoteURL)
				remoteAPIVersion, remoteLayer, err = fetchFromSource(req.RemoteURL)
			} else {
				remoteAPIVersion, remoteLayer, err = getSourceLAYER(localLayer, req.Force)
			}

			if err != nil {
				log.Printf("WARN: %v\n", err)
				return
			}

			if !req.Force && localLayer == remoteLayer {
				log.Println("INFO: No update required")
				return
			}

			log.Printf("INFO: Updating from layer %s to %s\n", localLayer, remoteLayer)
			remoteAPIVersion = cleanComments(remoteAPIVersion)

			file, err := os.OpenFile(tlLOC, os.O_RDWR|os.O_CREATE, 0600)
			if err != nil {
				log.Printf("ERROR: Failed to open schema file: %v\n", err)
				return
			}
			defer file.Close()

			file.Truncate(0)
			file.Seek(0, 0)
			file.WriteString(string(remoteAPIVersion))

			log.Println("INFO: Generating code from updated schema")
			if err := root(tlLOC, desLOC, req.Docs, remoteLayer); err != nil {
				log.Printf("ERROR: Generation failed: %s\n", err)
			} else {
				log.Println("INFO: Update completed successfully")
			}
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send existing logs
	logMutex.Lock()
	for _, msg := range logBuffer {
		fmt.Fprintf(w, "data: %s\n\n", escapeJSON(msg))
		flusher.Flush()
	}
	logMutex.Unlock()

	// Stream new logs
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case msg := <-logBroadcast:
			fmt.Fprintf(w, "data: %s\n\n", escapeJSON(msg))
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func handleMissingType(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case typeName := <-webUITypeHandler.RequestCh:
			req := MissingTypeRequest{Type: typeName}
			data, _ := json.Marshal(req)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func handleMissingTypeResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var resp MissingTypeResponse
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	webUITypeHandler.ResponseCh <- resp.Input

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func fetchFromSource(url string) ([]byte, string, error) {
	log.Printf("INFO: Fetching schema from: %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	// Try to extract layer
	reg := regexp.MustCompile(`// LAYER (\d+)`)
	matches := reg.FindStringSubmatch(string(data))
	layer := "unknown"
	if len(matches) > 1 {
		layer = matches[1]
	} else {
		// Try alternative format
		reg2 := regexp.MustCompile(`constexpr int32 MTPROTO_LAYER = (\d+);`)
		matches2 := reg2.FindStringSubmatch(string(data))
		if len(matches2) > 1 {
			layer = matches2[1]
		}
	}

	return data, layer, nil
}

func handleFetchRemote(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	data, layer, err := fetchFromSource(req.URL)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"layer": layer,
		"size":  len(data),
	})
}

func comparSchemas(local, remote *tlparser.Schema) SchemaDiff {
	diff := SchemaDiff{
		NewTypes:       []string{},
		DeletedTypes:   []string{},
		UpdatedTypes:   []string{},
		NewMethods:     []string{},
		DeletedMethods: []string{},
		UpdatedMethods: []string{},
	}

	localTypeMap := make(map[string]tlparser.Object)
	for _, obj := range local.Objects {
		localTypeMap[obj.Name] = obj
	}

	remoteTypeMap := make(map[string]tlparser.Object)
	for _, obj := range remote.Objects {
		remoteTypeMap[obj.Name] = obj
	}

	// Find new and updated types
	for name, remoteObj := range remoteTypeMap {
		if localObj, exists := localTypeMap[name]; exists {
			if localObj.CRC != remoteObj.CRC {
				diff.UpdatedTypes = append(diff.UpdatedTypes, name)
			}
		} else {
			diff.NewTypes = append(diff.NewTypes, name)
		}
	}

	// Find deleted types
	for name := range localTypeMap {
		if _, exists := remoteTypeMap[name]; !exists {
			diff.DeletedTypes = append(diff.DeletedTypes, name)
		}
	}

	// Compare methods
	localMethodMap := make(map[string]tlparser.Method)
	for _, method := range local.Methods {
		localMethodMap[method.Name] = method
	}

	remoteMethodMap := make(map[string]tlparser.Method)
	for _, method := range remote.Methods {
		remoteMethodMap[method.Name] = method
	}

	for name, remoteMethod := range remoteMethodMap {
		if localMethod, exists := localMethodMap[name]; exists {
			if localMethod.CRC != remoteMethod.CRC {
				diff.UpdatedMethods = append(diff.UpdatedMethods, name)
			}
		} else {
			diff.NewMethods = append(diff.NewMethods, name)
		}
	}

	for name := range localMethodMap {
		if _, exists := remoteMethodMap[name]; !exists {
			diff.DeletedMethods = append(diff.DeletedMethods, name)
		}
	}

	return diff
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>tlgen · gogram</title>
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.1/css/all.min.css">
<style>
@import url('https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap');
*{margin:0;padding:0;box-sizing:border-box}
:root{
  --bg:#fafafa;--surface:#fff;--surface-2:#fafbfc;--border:#e5e7eb;--border-soft:#eef0f3;
  --text:#0f172a;--muted:#64748b;--dim:#94a3b8;
  --accent:#2563eb;--accent-hover:#1e40af;--accent-soft:rgba(37,99,235,.12);
  --green:#059669;--red:#dc2626;--amber:#d97706;
  --input-bg:#fff;--hover:#f3f4f6;--chip-bg:#f1f5f9;
  --mono:'JetBrains Mono',ui-monospace,monospace;
}
:root[data-theme=dark]{
  --bg:#0d1117;--surface:#161b22;--surface-2:#1a1f29;--border:#21262d;--border-soft:#1c2128;
  --text:#e6edf3;--muted:#8b949e;--dim:#6e7681;
  --accent:#58a6ff;--accent-hover:#79b8ff;--accent-soft:rgba(88,166,255,.16);
  --green:#3fb950;--red:#f85149;--amber:#d29922;
  --input-bg:#0d1117;--hover:#1f262f;--chip-bg:#1f262f;
}
body{font-family:'Inter',-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:var(--bg);color:var(--text);line-height:1.5;font-size:13px}
.app{max-width:1180px;margin:0 auto;padding:18px 22px 64px}
.topbar{display:flex;align-items:center;justify-content:space-between;margin-bottom:18px;padding-bottom:14px;border-bottom:1px solid var(--border)}
.brand{display:flex;align-items:center;gap:10px;font-weight:600;font-size:13px;letter-spacing:-.01em}
.brand .sub{color:var(--muted);font-weight:400;font-size:12px;margin-left:4px}
.theme-toggle{background:transparent;color:var(--muted);border:1px solid var(--border);border-radius:5px;padding:5px 9px;font-size:11px;cursor:pointer;display:inline-flex;align-items:center;gap:5px}
.theme-toggle:hover{color:var(--text);background:var(--hover)}
.grid-two{display:grid;grid-template-columns:1.4fr 1fr;gap:16px;margin-bottom:16px}
@media (max-width:880px){.grid-two{grid-template-columns:1fr}}
.card{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:14px 16px}
.card h2{font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:.06em;color:var(--muted);margin-bottom:10px}
.layer-row{display:flex;align-items:baseline;gap:18px;margin-bottom:10px}
.layer-cell{display:flex;align-items:baseline;gap:6px}
.layer-label{font-size:10px;text-transform:uppercase;letter-spacing:.08em;color:var(--dim);font-weight:600}
.layer-val{font-family:var(--mono);font-size:18px;font-weight:600;color:var(--text)}
.layer-val.local{color:var(--green)}.layer-val.remote{color:var(--accent)}
.layer-arrow{color:var(--dim);font-size:11px}
.row-input{display:flex;gap:6px;align-items:center;margin-bottom:8px}
input[type=text],select{flex:1;padding:6px 9px;border:1px solid var(--border);border-radius:5px;font-family:var(--mono);font-size:12px;background:var(--input-bg);color:var(--text);outline:0;transition:border-color .15s,box-shadow .15s}
input[type=text]:focus,select:focus{border-color:var(--accent);box-shadow:0 0 0 2px var(--accent-soft)}
select{flex:0 1 auto;min-width:170px;cursor:pointer}
button{padding:6px 11px;background:var(--text);color:var(--bg);border:0;border-radius:5px;font:inherit;font-size:12px;font-weight:500;cursor:pointer;display:inline-flex;align-items:center;gap:5px;transition:background-color .15s,border-color .15s}
button:hover{opacity:.88}
button.primary{background:var(--accent);color:#fff}button.primary:hover{background:var(--accent-hover);opacity:1}
button.secondary{background:var(--surface);color:var(--text);border:1px solid var(--border)}
button.secondary:hover{background:var(--hover)}
button.ghost{background:transparent;color:var(--muted);border:0}button.ghost:hover{color:var(--text);background:var(--hover)}
button:disabled{opacity:.45;cursor:not-allowed}
.diff-summary{display:grid;grid-template-columns:repeat(6,1fr);gap:6px;margin-bottom:10px}
.diff-stat{padding:8px 10px;border:1px solid var(--border-soft);border-radius:5px;background:var(--surface-2)}
.diff-stat-num{font-family:var(--mono);font-size:15px;font-weight:600;color:var(--text);line-height:1}
.diff-stat-lbl{font-size:9px;text-transform:uppercase;letter-spacing:.05em;color:var(--dim);font-weight:600;margin-top:4px}
.diff-stat.green .diff-stat-num{color:var(--green)}
.diff-stat.red .diff-stat-num{color:var(--red)}
.diff-stat.amber .diff-stat-num{color:var(--amber)}
.diff-details{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:8px}
.diff-bucket{border:1px solid var(--border-soft);border-radius:5px;overflow:hidden;background:var(--surface-2)}
.diff-bucket-head{padding:6px 10px;background:var(--surface);border-bottom:1px solid var(--border-soft);display:flex;align-items:center;justify-content:space-between;font-size:10px;text-transform:uppercase;letter-spacing:.06em;color:var(--muted);font-weight:600}
.diff-bucket-count{font-family:var(--mono);color:var(--text)}
.diff-list{max-height:180px;overflow-y:auto;padding:4px 0}
.diff-list::-webkit-scrollbar{width:5px}
.diff-list::-webkit-scrollbar-thumb{background:var(--border);border-radius:3px}
.diff-list div{padding:2px 10px;font-family:var(--mono);font-size:11px;color:var(--text);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.diff-list .new{color:var(--green)}.diff-list .updated{color:var(--accent)}.diff-list .deleted{color:var(--red)}
.toolbar{display:flex;gap:6px;flex-wrap:wrap;align-items:center}
.flags{display:flex;gap:14px;flex-wrap:wrap;margin-bottom:10px;padding-top:4px}
.flags label{display:flex;align-items:center;gap:6px;cursor:pointer;font-size:12px;color:var(--text);user-select:none}
.flags input[type=checkbox]{width:14px;height:14px;cursor:pointer;accent-color:var(--accent)}
.flags .hint{color:var(--muted);font-size:11px}
.logs-section{margin-top:16px}
.log-panel{background:#0d1117;color:#c9d1d9;border-radius:6px;overflow:hidden;font-family:var(--mono);font-size:12px;border:1px solid #1a1f29}
.log-head{background:#161b22;border-bottom:1px solid #1a1f29;padding:6px 12px;display:flex;align-items:center;justify-content:space-between}
.log-head-title{font-size:10px;color:#8b949e;text-transform:uppercase;letter-spacing:.05em;font-weight:600}
.log-actions{display:flex;gap:4px}
.log-actions button{background:transparent;border:0;color:#8b949e;font-size:10px;padding:3px 7px;border-radius:3px}
.log-actions button:hover{background:#1f262f;color:#c9d1d9}
.log-content{padding:10px 14px;height:340px;overflow-y:auto;white-space:pre-wrap;word-wrap:break-word;line-height:1.5}
.log-content::-webkit-scrollbar{width:8px}
.log-content::-webkit-scrollbar-track{background:#0d1117}
.log-content::-webkit-scrollbar-thumb{background:#30363d;border-radius:4px;border:2px solid #0d1117}
.log-line{padding:1px 0;font-size:11.5px}
.log-info{color:#58a6ff}.log-warn{color:#d29922}.log-error{color:#f85149}
.modal{display:none;position:fixed;inset:0;background:rgba(15,23,42,.55);backdrop-filter:blur(3px);z-index:1000}
.modal-content{position:absolute;top:50%;left:50%;transform:translate(-50%,-50%);background:var(--surface);padding:20px 22px;border-radius:8px;max-width:600px;width:92%;box-shadow:0 18px 40px rgba(0,0,0,.25);border:1px solid var(--border)}
.modal-content h3{margin-bottom:10px;font-size:15px;color:var(--text);font-weight:600}
.modal-content textarea{width:100%;min-height:100px;padding:9px 11px;border:1px solid var(--border);border-radius:5px;font-family:var(--mono);font-size:12px;margin-bottom:10px;resize:vertical;outline:0}
.modal-content textarea:focus{border-color:var(--accent)}
.modal-content .hint{font-size:11px;color:var(--muted);margin-bottom:10px;line-height:1.55}
.modal-content .hint code{background:var(--chip-bg);padding:1px 5px;border-radius:3px;font-family:var(--mono);font-size:11px;color:var(--accent)}
.modal-actions{display:flex;gap:6px}
footer{margin-top:24px;padding-top:14px;border-top:1px solid var(--border);text-align:center;font-size:11px;color:var(--dim)}
footer a{color:var(--muted);text-decoration:none}
footer a:hover{color:var(--accent)}
.empty{padding:14px;text-align:center;color:var(--dim);font-size:12px;background:var(--surface-2);border:1px dashed var(--border-soft);border-radius:5px}
</style>
</head>
<body>
<div class="app">
  <div class="topbar">
    <div class="brand">
      tlgen
      <span class="sub">· gogram schema generator</span>
    </div>
    <button class="theme-toggle" onclick="toggleTheme()" id="themeToggle" title="Toggle theme">
      <i class="fas fa-moon" id="themeIcon"></i><span id="themeLabel">Dark</span>
    </button>
  </div>

  <div class="grid-two">
    <div class="card">
      <h2>Schema diff</h2>
      <div class="layer-row">
        <div class="layer-cell"><span class="layer-label">Local</span><span class="layer-val local" id="localLayer">—</span></div>
        <span class="layer-arrow">→</span>
        <div class="layer-cell"><span class="layer-label">Remote</span><span class="layer-val remote" id="remoteLayer">—</span></div>
      </div>
      <div class="row-input">
        <select id="diffSource" onchange="applySource('customUrl',this)">
          <option value="">Quick pick…</option>
          <option value="https://raw.githubusercontent.com/TGScheme/Schema/main/main_api.tl">TGScheme/Schema (main)</option>
          <option value="https://raw.githubusercontent.com/tdlib/td/refs/heads/master/td/generate/scheme/telegram_api.tl">tdlib/td (master)</option>
          <option value="https://raw.githubusercontent.com/telegramdesktop/tdesktop/dev/Telegram/SourceFiles/mtproto/scheme/api.tl">tdesktop (dev)</option>
        </select>
        <input type="text" id="customUrl" placeholder="https://… (or pick above)">
        <button onclick="loadDiff()" class="secondary"><i class="fas fa-arrows-rotate"></i>Compare</button>
      </div>
      <div class="diff-summary" id="diffSummary"></div>
      <div class="diff-details" id="diffResult"></div>
    </div>

    <div class="card">
      <h2>Generate</h2>
      <div class="flags">
        <label><input type="checkbox" id="flagDocs"> <span>Docs</span></label>
        <label><input type="checkbox" id="flagForce"> <span>Force</span></label>
        <label><input type="checkbox" id="flagLocal"> <span>Local schema</span></label>
      </div>
      <div class="row-input">
        <select id="genSource" onchange="applySource('genCustomUrl',this)">
          <option value="">Quick pick…</option>
          <option value="https://raw.githubusercontent.com/TGScheme/Schema/main/main_api.tl">TGScheme/Schema (main)</option>
          <option value="https://raw.githubusercontent.com/tdlib/td/refs/heads/master/td/generate/scheme/telegram_api.tl">tdlib/td (master)</option>
          <option value="https://raw.githubusercontent.com/telegramdesktop/tdesktop/dev/Telegram/SourceFiles/mtproto/scheme/api.tl">tdesktop (dev)</option>
        </select>
        <input type="text" id="genCustomUrl" placeholder="Custom URL (or pick above)">
      </div>
      <div class="toolbar">
        <button onclick="generate()" class="primary"><i class="fas fa-play"></i>Run</button>
      </div>
    </div>
  </div>

  <div class="card logs-section">
    <h2 style="margin-bottom:8px">Logs</h2>
    <div class="log-panel">
      <div class="log-head">
        <span class="log-head-title">stdout</span>
        <div class="log-actions"><button onclick="clearLogs()">clear</button></div>
      </div>
      <div id="logs" class="log-content"></div>
    </div>
  </div>

  <footer>© 2025 <a href="https://github.com/AmarnathCJD" target="_blank">@AmarnathCJD</a> · <a href="https://github.com/amarnathcjd/gogram" target="_blank">gogram</a></footer>
</div>

<div id="missingTypeModal" class="modal">
  <div class="modal-content">
    <h3>Missing type: <span id="missingTypeName" style="font-family:var(--mono);color:var(--accent)"></span></h3>
    <div class="hint">Enter a TL definition or Go type. Examples:<br>
      <code>payments.starGiftAuction#abc123 gifts:Vector&lt;Gift&gt; = StarGiftAuction;</code><br>
      <code>string</code> · <code>int32</code> · <code>[]byte</code>
    </div>
    <textarea id="missingTypeInput" placeholder="TL definition or Go type…"></textarea>
    <div class="modal-actions">
      <button onclick="submitMissingType()" class="primary">Submit</button>
      <button onclick="skipMissingType()" class="secondary">Skip (use any)</button>
    </div>
  </div>
</div>

<script>
let logsDiv=document.getElementById('logs'),diffResultDiv=document.getElementById('diffResult'),diffSummary=document.getElementById('diffSummary'),missingTypeModal=document.getElementById('missingTypeModal'),logSource=null,missingTypeSource=null;

function loadDiff(){
  const customUrl=document.getElementById('customUrl').value.trim();
  const url='/api/diff';
  const options=customUrl?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({remote_url:customUrl})}:{};
  fetch(url,options).then(r=>r.json()).then(data=>{
    if(data.error){diffResultDiv.innerHTML='<div class="empty" style="color:var(--red)">Error: '+data.error+'</div>';return}
    document.getElementById('localLayer').textContent=data.local_layer;
    document.getElementById('remoteLayer').textContent=data.remote_layer;
    diffSummary.innerHTML=summaryStat('New types',data.new_types,'green')+summaryStat('Updated types',data.updated_types,'amber')+summaryStat('Deleted types',data.deleted_types,'red')+summaryStat('New methods',data.new_methods,'green')+summaryStat('Updated methods',data.updated_methods,'amber')+summaryStat('Deleted methods',data.deleted_methods,'red');
    const sections=[
      ['New types',data.new_types,'new'],
      ['Updated types',data.updated_types,'updated'],
      ['Deleted types',data.deleted_types,'deleted'],
      ['New methods',data.new_methods,'new'],
      ['Updated methods',data.updated_methods,'updated'],
      ['Deleted methods',data.deleted_methods,'deleted'],
    ];
    diffResultDiv.innerHTML=sections.map(([t,i,c])=>formatBucket(t,i,c)).join('')||'<div class="empty">No differences detected.</div>';
  })
}
function summaryStat(label,items,tone){
  const n=(items||[]).length;
  return '<div class="diff-stat '+tone+'"><div class="diff-stat-num">'+n+'</div><div class="diff-stat-lbl">'+label+'</div></div>';
}
function formatBucket(title,items,className){
  if(!items||items.length===0)return '';
  return '<div class="diff-bucket"><div class="diff-bucket-head"><span>'+title+'</span><span class="diff-bucket-count">'+items.length+'</span></div><div class="diff-list">'+items.map(i=>'<div class="'+className+'">'+escapeHtml(i)+'</div>').join('')+'</div></div>';
}
function generate(){
  const customUrl=document.getElementById('genCustomUrl').value.trim();
  const req={
    force:document.getElementById('flagForce').checked,
    docs:document.getElementById('flagDocs').checked,
    local:document.getElementById('flagLocal').checked,
    remote_url:customUrl,
  };
  fetch('/api/generate',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(req)}).then(r=>r.json()).then(()=>{appendLog('[UI] Generation started…')})
}
function connectLogs(){
  if(logSource)logSource.close();
  logSource=new EventSource('/api/logs');
  logSource.onmessage=e=>{appendLog(JSON.parse(e.data))}
}
function connectMissingType(){
  if(missingTypeSource)missingTypeSource.close();
  missingTypeSource=new EventSource('/api/missing-type');
  missingTypeSource.onmessage=e=>{showMissingTypeModal(JSON.parse(e.data).type)}
}
function appendLog(line){
  const div=document.createElement('div');
  div.className='log-line';
  if(line.includes('ERROR'))div.classList.add('log-error');
  else if(line.includes('WARN'))div.classList.add('log-warn');
  else if(line.includes('INFO'))div.classList.add('log-info');
  div.textContent=line;
  logsDiv.appendChild(div);
  logsDiv.scrollTop=logsDiv.scrollHeight;
}
function clearLogs(){logsDiv.innerHTML=''}
function showMissingTypeModal(name){document.getElementById('missingTypeName').textContent=name;document.getElementById('missingTypeInput').value='';missingTypeModal.style.display='block'}
function submitMissingType(){
  const input=document.getElementById('missingTypeInput').value.trim();
  fetch('/api/missing-type/response',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({input:input||''})}).then(()=>{missingTypeModal.style.display='none'})
}
function skipMissingType(){submitMissingType()}
function escapeHtml(text){const d=document.createElement('div');d.textContent=text;return d.innerHTML}

function applySource(targetId,sel){
  if(!sel.value)return;
  document.getElementById(targetId).value=sel.value;
  if(targetId==='customUrl')loadDiff();
}

function setTheme(mode){
  document.documentElement.setAttribute('data-theme',mode);
  try{localStorage.setItem('tlgen-theme',mode)}catch(e){}
  const icon=document.getElementById('themeIcon');
  const label=document.getElementById('themeLabel');
  if(mode==='dark'){icon.className='fas fa-sun';label.textContent='Light'}
  else{icon.className='fas fa-moon';label.textContent='Dark'}
}
function toggleTheme(){
  const cur=document.documentElement.getAttribute('data-theme')||'light';
  setTheme(cur==='dark'?'light':'dark');
}
(function initTheme(){
  let saved='light';
  try{saved=localStorage.getItem('tlgen-theme')||(window.matchMedia&&window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light')}catch(e){}
  setTheme(saved);
})();

connectLogs();connectMissingType();loadDiff();
</script>
</body>
</html>`
