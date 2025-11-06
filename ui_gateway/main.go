package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
)

type cfg struct {
	NamingURL string
	Addr      string
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	c := cfg{
		NamingURL: getenv("NAMING_URL", "http://localhost:8000"),
		Addr:      getenv("ADDR", ":8080"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/api/upload", c.handleUpload)       // form POST
	mux.HandleFunc("/api/lookup", c.handleLookup)       // ?fileId=
	mux.HandleFunc("/api/download", c.handleProxyDownload) // proxy: ?fileId=&nodeUrl=

	log.Printf("UI Gateway running at %s (NAMING_URL=%s)", c.Addr, c.NamingURL)
	log.Fatal(http.ListenAndServe(c.Addr, logReq(mux)))
}

func logReq(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

/* ---------------- UI PAGE ---------------- */

var page = template.Must(template.New("index").Parse(`
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<title>DFS Mini — UI</title>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<style>
  body { font-family: system-ui, Arial, sans-serif; margin: 24px; max-width: 900px; }
  h1 { margin-top: 0; }
  .card { border:1px solid #ddd; border-radius:12px; padding:16px; margin-bottom:16px; }
  .row { display:flex; gap:12px; align-items:center; flex-wrap:wrap; }
  label { min-width: 120px; display:inline-block; }
  input[type="text"] { padding:8px; width: 320px; }
  input[type="file"] { padding:6px; }
  button { padding:10px 14px; border-radius:8px; border:1px solid #222; background:#111; color:#fff; cursor:pointer; }
  button:disabled { opacity:.5; cursor:default; }
  .muted { color:#666; font-size: 13px; }
  pre { background:#f7f7f7; padding:12px; border-radius:8px; overflow:auto; }
  .replicas a { margin-right: 8px; display:inline-block; }
</style>
</head>
<body>
<h1>DFS Mini — Web UI</h1>

<div class="card">
  <h2>Upload File (Allocate → Upload → Commit)</h2>
  <form id="uploadForm">
    <div class="row"><label>Nama File</label> <input type="text" id="filename" placeholder="laporan.pdf" required></div>
    <div class="row"><label>Pilih File</label> <input type="file" id="file" required></div>
    <div class="row"><label>&nbsp;</label> <button type="submit" id="btnUpload">Upload & Replicate</button></div>
  </form>
  <div class="muted">UI ini akan otomatis: hit <code>/allocate</code> → upload ke 2 node → <code>/commit</code>.</div>
  <div id="uploadResult"></div>
</div>

<div class="card">
  <h2>Lookup & Download</h2>
  <div class="row">
    <label>File ID</label> <input type="text" id="lookupId" placeholder="isi dari hasil upload">
    <button id="btnLookup">Lookup</button>
  </div>
  <div id="lookupResult"></div>
</div>

<script>
const $ = s => document.querySelector(s);

function fmtJson(o){ return '<pre>'+JSON.stringify(o, null, 2)+'</pre>'; }

$("#uploadForm").addEventListener("submit", async (e)=>{
  e.preventDefault();
  const filename = $("#filename").value.trim();
  const file = $("#file").files[0];
  if(!filename || !file){ alert("Isi filename dan pilih file."); return; }

  $("#btnUpload").disabled = true;
  $("#uploadResult").innerHTML = "Processing...";

  const form = new FormData();
  form.append("filename", filename);
  form.append("file", file);

  try {
    const res = await fetch("/api/upload", { method:"POST", body: form });
    const data = await res.json();
    $("#uploadResult").innerHTML = '<b>Upload Result</b>'+fmtJson(data)
      + (data.fileId ? ('<div>Quick Lookup: <a href="#" onclick="quickLookup(\''+data.fileId+'\')">'+data.fileId+'</a></div>') : '');
    if (data.fileId) { $("#lookupId").value = data.fileId; }
  } catch(err){
    $("#uploadResult").innerHTML = "Error: "+err;
  } finally {
    $("#btnUpload").disabled = false;
  }
});

async function quickLookup(fid){
  $("#lookupId").value = fid;
  await doLookup();
}

$("#btnLookup").addEventListener("click", async ()=>{
  await doLookup();
});

async function doLookup(){
  const fid = $("#lookupId").value.trim();
  if(!fid){ alert("Isi File ID."); return; }
  $("#lookupResult").innerHTML = "Loading...";
  try {
    const res = await fetch("/api/lookup?fileId="+encodeURIComponent(fid));
    const data = await res.json();
    let html = '<b>Replicas</b>'+fmtJson(data);
    if (Array.isArray(data)) {
      html += '<div class="replicas">';
      for (const r of data) {
  const nodeUrl = r.url ?? r.URL;       // dukung "url" atau "URL"
  const nodeId  = r.nodeId ?? r.NodeID; // dukung "nodeId" atau "NodeID"
  if (!nodeUrl) continue;               // guard: jangan bikin link undefined
  const url = '/api/download?fileId=' + encodeURIComponent(fid) +
              '&nodeUrl=' + encodeURIComponent(nodeUrl);
  html += '<a href="'+url+'" target="_blank"><button>Download via '+nodeId+'</button></a>';
}
      html += '</div>';
    }
    $("#lookupResult").innerHTML = html;
  } catch(err){
    $("#lookupResult").innerHTML = "Error: "+err;
  }
}
</script>
</body>
</html>
`))

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(w, nil)
}

/* ---------------- API: UPLOAD ---------------- */

type allocateResp struct {
	FileID   string `json:"fileId"`
	Replicas []struct {
		NodeID string `json:"nodeId"`
		URL    string `json:"url"`
	} `json:"replicas"`
}

func (c cfg) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "parse form error", http.StatusBadRequest); return
	}
	filename := r.FormValue("filename")
	file, hdr, err := r.FormFile("file")
	if err != nil || filename == "" {
		http.Error(w, "missing filename/file", http.StatusBadRequest); return
	}
	defer file.Close()

	// read file into memory (for demo). Untuk file besar, lebih baik stream temp file.
	buf := &bytes.Buffer{}
	h := sha256.New()
	size, _ := io.Copy(io.MultiWriter(buf, h), file)
	checksum := "sha256:" + hex.EncodeToString(h.Sum(nil))

	// 1) allocate
	payload := map[string]any{
		"filename":    filename,
		"size":        size,
		"checksum":    checksum,
		"contentType": hdr.Header.Get("Content-Type"),
	}
	alloc, err := postJSON[allocateResp](c.NamingURL+"/allocate", payload)
	if err != nil {
		http.Error(w, "allocate error: "+err.Error(), http.StatusBadRequest); return
	}

	// 2) upload to each replica
	uploadedIDs := make([]string, 0, len(alloc.Replicas))
	for _, rep := range alloc.Replicas {
		if err := postMultipart(rep.URL+"/upload", alloc.FileID, filename, buf.Bytes()); err != nil {
			// skip failed node (client-driven best-effort)
			continue
		}
		uploadedIDs = append(uploadedIDs, rep.NodeID)
	}

	// 3) commit
	commitBody := map[string]any{
		"fileId":   alloc.FileID,
		"uploaded": uploadedIDs,
	}
	var commitResp map[string]any
	commitResp, _ = postJSON[map[string]any](c.NamingURL+"/commit", commitBody)

	writeJSON(w, map[string]any{
		"fileId":   alloc.FileID,
		"filename": filename,
		"size":     size,
		"checksum": checksum,
		"uploaded": uploadedIDs,
		"commit":   commitResp,
	})
}

func postMultipart(url, fileID, filename string, content []byte) error {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	_ = w.WriteField("fileId", fileID)
	fw, _ := w.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	w.Close()

	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	client := &http.Client{ Timeout: 15 * time.Second }
	resp, err := client.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload %s failed: %s", url, strings.TrimSpace(string(b)))
	}
	return nil
}

func postJSON[T any](url string, v any) (T, error) {
	var zero T
	b, _ := json.Marshal(v)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{ Timeout: 10 * time.Second }
	resp, err := client.Do(req)
	if err != nil { return zero, err }
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		x, _ := io.ReadAll(resp.Body)
		return zero, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(x)))
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&zero); err != nil { return zero, err }
	return zero, nil
}

/* ---------------- API: LOOKUP & DOWNLOAD ---------------- */

func (c cfg) handleLookup(w http.ResponseWriter, r *http.Request) {
	fid := r.URL.Query().Get("fileId")
	if fid == "" { http.Error(w, "missing fileId", http.StatusBadRequest); return }

	// panggil naming
	resp, err := http.Get(c.NamingURL + "/lookup/" + fid)
	if err != nil { http.Error(w, "lookup error: "+err.Error(), 500); return }
	defer resp.Body.Close()

	// baca body
	b, _ := io.ReadAll(resp.Body)
	// bentuk aslinya pakai "NodeID"/"URL"
	type in struct {
		NodeID string `json:"NodeID"`
		URL    string `json:"URL"`
	}
	var arr []in
	_ = json.Unmarshal(b, &arr)

	// normalisasi jadi "nodeId"/"url"
	type out struct {
		NodeId string `json:"nodeId"`
		Url    string `json:"url"`
	}
	outArr := make([]out, 0, len(arr))
	for _, v := range arr {
		outArr = append(outArr, out{NodeId: v.NodeID, Url: v.URL})
	}

	w.Header().Set("Content-Type", "application/json")
	if resp.StatusCode/100 != 2 {
		w.WriteHeader(resp.StatusCode)
		w.Write(b) // error dari naming apa adanya
		return
	}
	_ = json.NewEncoder(w).Encode(outArr)
}


func (c cfg) handleProxyDownload(w http.ResponseWriter, r *http.Request) {
	fid := r.URL.Query().Get("fileId")
	nodeURL := r.URL.Query().Get("nodeUrl")
	if fid == "" || nodeURL == "" {
		http.Error(w, "missing fileId or nodeUrl", http.StatusBadRequest); return
	}
	u := strings.TrimRight(nodeURL, "/") + "/download/" + fid
	resp, err := http.Get(u)
	if err != nil { http.Error(w, "download failed: "+err.Error(), 502); return }
	defer resp.Body.Close()

	// pass through headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

/* ---------------- JSON RESP ---------------- */

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
