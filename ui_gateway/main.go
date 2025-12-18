package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type cfg struct {
	NamingURL string
	Addr      string
	sys       *systemProc
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
		sys:       newSystemProc(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/dashboard", serveDashboard)
	mux.HandleFunc("/api/upload", c.handleUpload)          // form POST
	mux.HandleFunc("/api/lookup", c.handleLookup)          // ?fileId=
	mux.HandleFunc("/api/download", c.handleProxyDownload) // proxy: ?fileId=&nodeUrl=
	mux.HandleFunc("/api/files", c.handleListFiles)        // GET all files
	mux.HandleFunc("/api/nodes", c.handleListNodes)        // GET all nodes
	mux.HandleFunc("/api/metrics", c.handleMetrics)        // GET system metrics
	mux.HandleFunc("/api/delete", c.handleDeleteFile)      // DELETE file
	mux.HandleFunc("/api/search", c.handleSearch)          // search files by id/name
	mux.HandleFunc("/api/system/start", c.handleSystemStart)
	mux.HandleFunc("/api/system/stop", c.handleSystemStop)
	mux.HandleFunc("/api/system/status", c.handleSystemStatus)
	mux.HandleFunc("/api/system/stop-node", c.handleStopNode)
	mux.HandleFunc("/api/system/start-node", c.handleStartNode)

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

func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "dashboard.html")
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
		http.Error(w, "parse form error", http.StatusBadRequest)
		return
	}
	filename := r.FormValue("filename")
	file, hdr, err := r.FormFile("file")
	if err != nil || filename == "" {
		http.Error(w, "missing filename/file", http.StatusBadRequest)
		return
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "allocate error", "detail": err.Error()})
		return
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

	// <-- INSERT REQUIRED-WRITES CHECK HERE (before commit) -->
	requiredWrites := 2
	if len(uploadedIDs) < requiredWrites {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "not enough replicas uploaded",
			"detail": fmt.Sprintf("uploaded %d, required %d", len(uploadedIDs), requiredWrites),
		})
		return
	}
	// <-- end check -->

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
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
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
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		x, _ := io.ReadAll(resp.Body)
		return zero, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(x)))
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&zero); err != nil {
		return zero, err
	}
	return zero, nil
}

/* ---------------- API: LOOKUP & DOWNLOAD ---------------- */

func (c cfg) handleLookup(w http.ResponseWriter, r *http.Request) {
	fid := r.URL.Query().Get("fileId")
	if fid == "" {
		http.Error(w, "missing fileId", http.StatusBadRequest)
		return
	}

	// panggil naming
	resp, err := http.Get(c.NamingURL + "/lookup/" + fid)
	if err != nil {
		http.Error(w, "lookup error: "+err.Error(), 500)
		return
	}
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
		http.Error(w, "missing fileId or nodeUrl", http.StatusBadRequest)
		return
	}
	u := strings.TrimRight(nodeURL, "/") + "/download/" + fid
	resp, err := http.Get(u)
	if err != nil {
		http.Error(w, "download failed: "+err.Error(), 502)
		return
	}
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

/* ---------------- ADMIN API ---------------- */

func (c cfg) handleListFiles(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(c.NamingURL + "/list-files")
	if err != nil {
		w.WriteHeader(500)
		writeJSON(w, map[string]string{"error": "failed to get files"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		w.WriteHeader(resp.StatusCode)
		writeJSON(w, map[string]string{"error": "upstream error"})
		return
	}
	io.Copy(w, resp.Body)
}

func (c cfg) handleListNodes(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(c.NamingURL + "/list-nodes")
	if err != nil {
		w.WriteHeader(500)
		writeJSON(w, map[string]string{"error": "failed to get nodes"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		w.WriteHeader(resp.StatusCode)
		writeJSON(w, map[string]string{"error": "upstream error"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func (c cfg) handleMetrics(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(c.NamingURL + "/metrics")
	if err != nil {
		w.WriteHeader(500)
		writeJSON(w, map[string]string{"error": "failed to get metrics"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		w.WriteHeader(resp.StatusCode)
		writeJSON(w, map[string]string{"error": "upstream error"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func (c cfg) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	fid := body["fileId"]
	if fid == "" {
		http.Error(w, "missing fileId", 400)
		return
	}
	lr, err := http.Get(c.NamingURL + "/lookup/" + fid)
	var replicas []struct{ NodeID, URL string }
	if err == nil {
		defer lr.Body.Close()
		_ = json.NewDecoder(lr.Body).Decode(&replicas)
	}
	deletedNodes := []string{}
	for _, rep := range replicas {
		reqBody := map[string]string{"fileId": fid}
		rb, _ := json.Marshal(reqBody)
		u := strings.TrimRight(rep.URL, "/") + "/delete"
		cli := &http.Client{Timeout: 2 * time.Second}
		rr, err := cli.Post(u, "application/json", bytes.NewReader(rb))
		if err == nil {
			deletedNodes = append(deletedNodes, rep.NodeID)
			if rr != nil {
				rr.Body.Close()
			}
		}
	}
	nb, _ := json.Marshal(map[string]string{"fileId": fid})
	dr, err := http.Post(c.NamingURL+"/delete-file", "application/json", bytes.NewReader(nb))
	if err != nil {
		http.Error(w, "delete failed", 500)
		return
	}
	defer dr.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"fileId": fid, "deleted": true, "nodes": deletedNodes})
}

func (c cfg) handleSearch(w http.ResponseWriter, r *http.Request) {
	qfid := r.URL.Query().Get("fileId")
	qname := r.URL.Query().Get("filename")
	q := r.URL.Query().Get("q")
	if q != "" {
		if qfid == "" {
			qfid = q
		}
		if qname == "" {
			qname = q
		}
	}
	resp, err := http.Get(c.NamingURL + "/list-files")
	if err != nil {
		http.Error(w, "failed to get files", 500)
		return
	}
	defer resp.Body.Close()
	var files []struct {
		FileID       string `json:"fileId"`
		Filename     string `json:"filename"`
		Size         int64  `json:"size"`
		State        string `json:"state"`
		ReplicaCount int    `json:"replicaCount"`
		CreatedAt    string `json:"createdAt"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&files); err != nil {
		http.Error(w, "bad response", 502)
		return
	}
	var out []any
	for _, f := range files {
		if qfid == "" && qname == "" {
			out = append(out, f)
			continue
		}
		match := false
		if qfid != "" && strings.Contains(strings.ToLower(f.FileID), strings.ToLower(qfid)) {
			match = true
		}
		if qname != "" && strings.Contains(strings.ToLower(f.Filename), strings.ToLower(qname)) {
			match = true
		}
		if match {
			out = append(out, f)
		}
	}
	writeJSON(w, out)
}

type systemProc struct {
	mu     sync.Mutex
	naming *exec.Cmd
	nodeA  *exec.Cmd
	nodeB  *exec.Cmd
}

func newSystemProc() *systemProc { return &systemProc{} }

func (s *systemProc) isRunning(cmd *exec.Cmd) bool { return cmd != nil && cmd.Process != nil }

func (s *systemProc) startAll() (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	os.MkdirAll(filepath.Join("..", "logs"), 0755)
	if s.naming == nil || s.naming.Process == nil {
		s.naming = exec.Command("go", "run", "main.go")
		s.naming.Dir = filepath.Join("..", "naming_service")
		f, _ := os.OpenFile(filepath.Join("..", "logs", "naming.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		s.naming.Stdout = f
		s.naming.Stderr = f
		_ = s.naming.Start()
	}
	if s.nodeA == nil || s.nodeA.Process == nil {
		s.nodeA = exec.Command("go", "run", "main.go")
		s.nodeA.Dir = filepath.Join("..", "storage_node")
		s.nodeA.Env = append(os.Environ(),
			"NODE_ID=node-a",
			"PORT=9001",
			"DATA_DIR=./data_a",
			"NAMING_URL=http://localhost:8000",
			"CAPACITY_BYTES=1073741824",
		)
		f, _ := os.OpenFile(filepath.Join("..", "logs", "node-a.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		s.nodeA.Stdout = f
		s.nodeA.Stderr = f
		_ = s.nodeA.Start()
	}
	if s.nodeB == nil || s.nodeB.Process == nil {
		s.nodeB = exec.Command("go", "run", "main.go")
		s.nodeB.Dir = filepath.Join("..", "storage_node")
		s.nodeB.Env = append(os.Environ(),
			"NODE_ID=node-b",
			"PORT=9002",
			"DATA_DIR=./data_b",
			"NAMING_URL=http://localhost:8000",
			"CAPACITY_BYTES=1073741824",
		)
		f, _ := os.OpenFile(filepath.Join("..", "logs", "node-b.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		s.nodeB.Stdout = f
		s.nodeB.Stderr = f
		_ = s.nodeB.Start()
	}
	return map[string]bool{
		"naming": s.isRunning(s.naming),
		"nodeA":  s.isRunning(s.nodeA),
		"nodeB":  s.isRunning(s.nodeB),
	}, nil
}

func (s *systemProc) stopAll() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	stopped := map[string]bool{"naming": false, "nodeA": false, "nodeB": false}
	if s.naming != nil && s.naming.Process != nil {
		_ = s.naming.Process.Kill()
		stopped["naming"] = true
		s.naming = nil
	}
	if s.nodeA != nil && s.nodeA.Process != nil {
		_ = s.nodeA.Process.Kill()
		stopped["nodeA"] = true
		s.nodeA = nil
	}
	if s.nodeB != nil && s.nodeB.Process != nil {
		_ = s.nodeB.Process.Kill()
		stopped["nodeB"] = true
		s.nodeB = nil
	}
	return stopped
}

func (s *systemProc) startNode(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch id {
	case "node-a":
		if (s.nodeA != nil && s.nodeA.Process != nil) && ping("http://localhost:9001/health") {
			return true
		}
		time.Sleep(300 * time.Millisecond)
		s.nodeA = exec.Command("go", "run", "main.go")
		s.nodeA.Dir = filepath.Join("..", "storage_node")
		s.nodeA.Env = append(os.Environ(),
			"NODE_ID=node-a", "PORT=9001", "DATA_DIR=./data_a", "NAMING_URL=http://localhost:8000", "CAPACITY_BYTES=1073741824",
		)
		f, _ := os.OpenFile(filepath.Join("..", "logs", "node-a.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		s.nodeA.Stdout = f
		s.nodeA.Stderr = f
		_ = s.nodeA.Start()
		return true
	case "node-b":
		if (s.nodeB != nil && s.nodeB.Process != nil) && ping("http://localhost:9002/health") {
			return true
		}
		time.Sleep(300 * time.Millisecond)
		s.nodeB = exec.Command("go", "run", "main.go")
		s.nodeB.Dir = filepath.Join("..", "storage_node")
		s.nodeB.Env = append(os.Environ(),
			"NODE_ID=node-b", "PORT=9002", "DATA_DIR=./data_b", "NAMING_URL=http://localhost:8000", "CAPACITY_BYTES=1073741824",
		)
		f, _ := os.OpenFile(filepath.Join("..", "logs", "node-b.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		s.nodeB.Stdout = f
		s.nodeB.Stderr = f
		_ = s.nodeB.Start()
		return true
	default:
		return false
	}
}

func (c cfg) handleSystemStart(w http.ResponseWriter, r *http.Request) {
	status, _ := c.sys.startAll()
	writeJSON(w, map[string]any{"started": true, "status": status})
}
func (c cfg) handleSystemStop(w http.ResponseWriter, r *http.Request) {
	status := c.sys.stopAll()
	writeJSON(w, map[string]any{"stopped": true, "status": status})
}
func (c cfg) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"naming": ping(c.NamingURL+"/metrics") || c.sys.isRunning(c.sys.naming),
		"nodeA":  ping("http://localhost:9001/health") || c.sys.isRunning(c.sys.nodeA),
		"nodeB":  ping("http://localhost:9002/health") || c.sys.isRunning(c.sys.nodeB),
	})
}

func ping(url string) bool {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

func (c cfg) handleStopNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"nodeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NodeID == "" {
		http.Error(w, "bad json", 400)
		return
	}
	resp, err := http.Get(c.NamingURL + "/list-nodes")
	if err != nil {
		http.Error(w, "cannot list nodes", 500)
		return
	}
	defer resp.Body.Close()
	var nodes []struct{ NodeID, URL string }
	_ = json.NewDecoder(resp.Body).Decode(&nodes)
	var target string
	for _, n := range nodes {
		if n.NodeID == body.NodeID {
			target = n.URL
			break
		}
	}
	if target == "" {
		http.Error(w, "node not found", 404)
		return
	}
	req, _ := http.NewRequest("POST", strings.TrimRight(target, "/")+"/shutdown", nil)
	res, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		http.Error(w, "shutdown failed", 502)
		return
	}
	defer res.Body.Close()
	c.sys.mu.Lock()
	if body.NodeID == "node-a" {
		c.sys.nodeA = nil
	}
	if body.NodeID == "node-b" {
		c.sys.nodeB = nil
	}
	c.sys.mu.Unlock()
	writeJSON(w, map[string]any{"nodeId": body.NodeID, "stopped": true})
}

func (c cfg) handleStartNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"nodeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NodeID == "" {
		http.Error(w, "bad json", 400)
		return
	}
	ok := c.sys.startNode(body.NodeID)
	writeJSON(w, map[string]any{"nodeId": body.NodeID, "started": ok})
}
