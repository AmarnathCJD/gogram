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

const indexHTML = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>TL Generator - Gogram</title><link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.1/css/all.min.css"><style>@import url('https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap');*{margin:0;padding:0;box-sizing:border-box}body{font-family:'Inter',-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#f8f9fa;color:#1a1a1a;line-height:1.6;padding:20px 20px 60px;min-height:100vh;position:relative}.container{max-width:1400px;margin:0 auto}.header{display:flex;align-items:center;gap:16px;margin-bottom:32px}.icon{width:48px;height:48px;background:linear-gradient(135deg,#667eea 0%,#764ba2 100%);border-radius:12px;display:flex;align-items:center;justify-content:center;font-size:24px;color:white;font-weight:bold;box-shadow:0 4px 12px rgba(102,126,234,0.3)}.header-text h1{margin:0;font-size:28px;font-weight:700;letter-spacing:-0.5px;color:#111827}.header-text .subtitle{color:#6b7280;font-size:14px;margin:4px 0 0}h2{margin:0 0 18px;font-size:16px;font-weight:600;color:#374151;text-transform:uppercase;letter-spacing:0.5px;font-size:13px}.section{margin-bottom:20px;padding:24px;background:#fff;border:1px solid #e5e7eb;border-radius:10px;box-shadow:0 1px 2px rgba(0,0,0,0.04)}.input-group{display:flex;gap:8px;margin-bottom:16px;align-items:center}.input-group input[type="text"]{flex:1;padding:10px 14px;border:1px solid #d4d4d4;border-radius:8px;font-family:'JetBrains Mono',monospace;font-size:13px;transition:border-color 0.2s}.input-group input[type="text"]:focus{outline:none;border-color:#2563eb}.btn-group{display:flex;gap:10px;flex-wrap:wrap}button{padding:10px 20px;background:#2563eb;color:#fff;border:none;border-radius:8px;cursor:pointer;font-size:14px;font-weight:500;font-family:'Inter',sans-serif;transition:all 0.2s}button:hover{background:#1d4ed8;transform:translateY(-1px);box-shadow:0 4px 12px rgba(37,99,235,0.2)}button:disabled{background:#d4d4d4;cursor:not-allowed;transform:none;box-shadow:none}button.secondary{background:#f5f5f5;color:#2c2c2c;border:1px solid #e5e5e5}button.secondary:hover{background:#ebebeb;box-shadow:0 2px 8px rgba(0,0,0,0.08)}.checkbox-group{display:flex;gap:20px;margin-bottom:16px;flex-wrap:wrap}.checkbox-group label{display:flex;align-items:center;gap:8px;cursor:pointer;font-size:14px;color:#404040}.checkbox-group input[type="checkbox"]{width:18px;height:18px;cursor:pointer}.diff-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px}.diff-item{padding:16px;background:#f9fafb;border:1px solid #e5e7eb;border-radius:10px}.diff-item h3{font-size:13px;margin-bottom:12px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;color:#6b7280}.diff-list{max-height:220px;overflow-y:auto;font-size:13px}.diff-list::-webkit-scrollbar{width:6px}.diff-list::-webkit-scrollbar-thumb{background:#d4d4d4;border-radius:3px}.diff-list div{padding:5px 0;font-family:'JetBrains Mono',monospace;font-size:12px}.new{color:#059669;font-weight:500}.deleted{color:#dc2626;font-weight:500}.updated{color:#2563eb;font-weight:500}.log-panel{background:#0d1117;color:#c9d1d9;padding:0;border-radius:8px;height:480px;overflow:hidden;font-family:'JetBrains Mono',monospace;font-size:13px;line-height:1.5;border:1px solid #30363d}.log-header{background:#161b22;border-bottom:1px solid #30363d;padding:10px 16px;display:flex;align-items:center;justify-content:space-between}.log-header-title{font-size:12px;color:#8b949e;font-weight:500;text-transform:uppercase;letter-spacing:0.5px}.log-content{padding:16px;height:calc(480px - 42px);overflow-y:auto;white-space:pre-wrap;word-wrap:break-word}.log-content::-webkit-scrollbar{width:10px}.log-content::-webkit-scrollbar-track{background:#0d1117}.log-content::-webkit-scrollbar-thumb{background:#30363d;border-radius:5px;border:2px solid #0d1117}.log-content::-webkit-scrollbar-thumb:hover{background:#484f58}.log-line{margin:0;padding:4px 0;font-size:12.5px}.log-info{color:#58a6ff}.log-warn{color:#d29922}.log-error{color:#f85149}.layer-info{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:16px;margin-bottom:20px}.layer-card{background:white;border-radius:8px;padding:20px;border:1px solid #e5e7eb;box-shadow:0 1px 3px 0 rgba(0,0,0,0.1);transition:all 0.2s ease}.layer-card:hover{box-shadow:0 4px 6px -1px rgba(0,0,0,0.1),0 2px 4px -1px rgba(0,0,0,0.06);border-color:#d1d5db}.layer-card-label{font-size:11px;text-transform:uppercase;letter-spacing:0.8px;color:#6b7280;font-weight:600;margin-bottom:8px}.layer-card-value{font-family:'JetBrains Mono',monospace;font-size:28px;font-weight:700;color:#1f2937}.layer-card.local .layer-card-value{color:#059669}.layer-card.remote .layer-card-value{color:#2563eb}.missing-type-modal{display:none;position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.6);backdrop-filter:blur(4px);z-index:1000}.missing-type-content{position:absolute;top:50%;left:50%;transform:translate(-50%,-50%);background:#fff;padding:28px;border-radius:16px;max-width:650px;width:90%;box-shadow:0 20px 50px rgba(0,0,0,0.3)}.missing-type-content h3{margin-bottom:16px;font-size:20px;color:#1a1a1a}.missing-type-content textarea{width:100%;min-height:120px;padding:12px;border:1px solid #d4d4d4;border-radius:8px;font-family:'JetBrains Mono',monospace;font-size:13px;margin-bottom:16px;resize:vertical}.missing-type-content textarea:focus{outline:none;border-color:#2563eb}.missing-type-content .hint{font-size:13px;color:#6b7280;margin-bottom:16px;line-height:1.7}.missing-type-content .hint code{background:#f5f5f5;padding:2px 6px;border-radius:4px;font-family:'JetBrains Mono',monospace;font-size:12px;color:#dc2626}@media (max-width:768px){.diff-grid{grid-template-columns:1fr}.layer-info{grid-template-columns:1fr}.btn-group{flex-direction:column}button{width:100%}}footer{position:fixed;bottom:0;left:0;right:0;background:white;border-top:1px solid #e5e7eb;padding:16px 20px;text-align:right;font-size:13px;color:#6b7280;box-shadow:0 -1px 3px 0 rgba(0,0,0,0.1)}footer a{color:#667eea;text-decoration:none;font-weight:500}footer a:hover{text-decoration:underline}</style></head><body><div class="container"><div class="header"><div class="icon"><i class="fas fa-code"></i></div><div class="header-text"><h1>TL Generator</h1><div class="subtitle">Telegram Type Language Schema Generator for Gogram</div></div></div><div class="section"><h2>Schema Comparison</h2><div class="layer-info"><div class="layer-card local"><div class="layer-card-label">Local Layer</div><div class="layer-card-value" id="localLayer">-</div></div><div class="layer-card remote"><div class="layer-card-label">Remote Layer</div><div class="layer-card-value" id="remoteLayer">-</div></div></div><div class="input-group"><input type="text" id="customUrl" placeholder="Custom remote URL (optional)"><button onclick="loadDiff()" class="secondary">Compare</button></div><div id="diffResult" class="diff-grid"></div></div><div class="section"><h2>Generation Options</h2><div class="checkbox-group"><label><input type="checkbox" id="flagDocs"> Generate Docs</label><label><input type="checkbox" id="flagForce"> Force Update</label><label><input type="checkbox" id="flagLocal"> Use Local Schema</label></div><div class="input-group"><input type="text" id="genCustomUrl" placeholder="Custom remote URL (optional)"><button onclick="generate()">Start Generation</button></div></div><div class="section"><h2>Logs</h2><button onclick="clearLogs()" class="secondary" style="margin-bottom:12px">Clear Logs</button><div class="log-panel"><div class="log-header"><span class="log-header-title">Console Output</span></div><div id="logs" class="log-content"></div></div></div></div><footer>Copyright © 2025 <a href="https://github.com/AmarnathCJD" target="_blank">AmarnathCJD</a></footer><div id="missingTypeModal" class="missing-type-modal"><div class="missing-type-content"><h3>Missing Type: <span id="missingTypeName"></span></h3><div class="hint">Enter TL definition or Go type name:<br>Example: <code>payments.starGiftAuction#abc123 gifts:Vector&lt;Gift&gt; = StarGiftAuction;</code><br>Or: <code>string</code>, <code>int32</code>, <code>[]byte</code></div><textarea id="missingTypeInput" placeholder="Enter TL definition or Go type..."></textarea><div class="btn-group"><button onclick="submitMissingType()">Submit</button><button onclick="skipMissingType()" class="secondary">Skip (use interface{})</button></div></div></div><script>let logsDiv=document.getElementById('logs'),diffResultDiv=document.getElementById('diffResult'),missingTypeModal=document.getElementById('missingTypeModal'),logSource=null,missingTypeSource=null;function loadDiff(){const customUrl=document.getElementById('customUrl').value.trim(),url='/api/diff',options=customUrl?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({remote_url:customUrl})}:{};fetch(url,options).then(r=>r.json()).then(data=>{if(data.error){diffResultDiv.innerHTML='<div style="color:#d00">Error: '+data.error+'</div>';return}document.getElementById('localLayer').textContent=data.local_layer;document.getElementById('remoteLayer').textContent=data.remote_layer;diffResultDiv.innerHTML=formatDiffSection('New Types',data.new_types,'new')+formatDiffSection('Deleted Types',data.deleted_types,'deleted')+formatDiffSection('Updated Types',data.updated_types,'updated')+formatDiffSection('New Methods',data.new_methods,'new')+formatDiffSection('Deleted Methods',data.deleted_methods,'deleted')+formatDiffSection('Updated Methods',data.updated_methods,'updated')})}function formatDiffSection(title,items,className){if(!items||items.length===0)return '';return '<div class="diff-item"><h3>'+title+' ('+items.length+')</h3><div class="diff-list">'+items.map(i=>'<div class="'+className+'">'+escapeHtml(i)+'</div>').join('')+'</div></div>'}function generate(){const customUrl=document.getElementById('genCustomUrl').value.trim(),req={force:document.getElementById('flagForce').checked,docs:document.getElementById('flagDocs').checked,local:document.getElementById('flagLocal').checked,remote_url:customUrl};fetch('/api/generate',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(req)}).then(r=>r.json()).then(data=>{appendLog('[UI] Generation started...')})}function connectLogs(){if(logSource)logSource.close();logSource=new EventSource('/api/logs');logSource.onmessage=e=>{const line=JSON.parse(e.data);appendLog(line)}}function connectMissingType(){if(missingTypeSource)missingTypeSource.close();missingTypeSource=new EventSource('/api/missing-type');missingTypeSource.onmessage=e=>{const data=JSON.parse(e.data);showMissingTypeModal(data.type)}}function appendLog(line){const div=document.createElement('div');div.className='log-line';if(line.includes('ERROR'))div.classList.add('log-error');else if(line.includes('WARN'))div.classList.add('log-warn');else if(line.includes('INFO'))div.classList.add('log-info');div.textContent=line;logsDiv.appendChild(div);logsDiv.scrollTop=logsDiv.scrollHeight}function clearLogs(){logsDiv.innerHTML=''}function showMissingTypeModal(typeName){document.getElementById('missingTypeName').textContent=typeName;document.getElementById('missingTypeInput').value='';missingTypeModal.style.display='block'}function submitMissingType(){const input=document.getElementById('missingTypeInput').value.trim();fetch('/api/missing-type/response',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({input:input||''})}).then(()=>{missingTypeModal.style.display='none'})}function skipMissingType(){submitMissingType()}function escapeHtml(text){const div=document.createElement('div');div.textContent=text;return div.innerHTML}connectLogs();connectMissingType();loadDiff()</script></body></html>`
