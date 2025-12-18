package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Node struct {
	NodeID        string
	Port          string
	DataDir       string
	NamingURL     string
	CapacityBytes int64
	mu            sync.RWMutex
	usedBytes     int64
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func (n *Node) dataPathFor(fileID string) string {
	if len(fileID) < 2 {
		return filepath.Join(n.DataDir, fileID)
	}
	sub := fileID[:2]
	dir := filepath.Join(n.DataDir, sub)
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, fileID)
}
func (n *Node) addUsed(delta int64) {
	n.mu.Lock()
	n.usedBytes += delta
	if n.usedBytes < 0 {
		n.usedBytes = 0
	}
	n.mu.Unlock()
}
func (n *Node) currentUsed() int64 { n.mu.RLock(); defer n.mu.RUnlock(); return n.usedBytes }

func (n *Node) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "parse form", 400)
		return
	}
	fileID := r.FormValue("fileId")
	if fileID == "" {
		http.Error(w, "missing fileId", 400)
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", 400)
		return
	}
	defer f.Close()

	target := n.dataPathFor(fileID)
	out, err := os.Create(target)
	if err != nil {
		http.Error(w, "cannot create", 500)
		return
	}
	defer out.Close()

	h := sha256.New()
	size, err := copyWithHash(out, f, h)
	if err != nil {
		http.Error(w, "write error", 500)
		return
	}
	n.addUsed(size)
	checksum := "sha256:" + hex.EncodeToString(h.Sum(nil))
	writeJSON(w, map[string]any{"ok": true, "fileId": fileID, "size": size, "checksum": checksum, "name": hdr.Filename})
}
func copyWithHash(dst io.Writer, src multipart.File, h io.Writer) (int64, error) {
	return io.Copy(io.MultiWriter(dst, h), src)
}

func (n *Node) handleDownload(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/download/")
	if fileID == "" {
		http.Error(w, "missing fileId", 400)
		return
	}
	path := n.dataPathFor(fileID)
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	defer f.Close()
	http.ServeContent(w, r, fileID, time.Now(), f)
}

func (n *Node) handleHas(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("fileId")
	if fileID == "" {
		http.Error(w, "missing fileId", 400)
		return
	}
	_, err := os.Stat(n.dataPathFor(fileID))
	writeJSON(w, map[string]any{"exists": err == nil})
}
func (n *Node) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"nodeId":        n.NodeID,
		"status":        "HEALTHY",
		"usedBytes":     n.currentUsed(),
		"capacityBytes": n.CapacityBytes,
		"freeBytes":     n.CapacityBytes - n.currentUsed(),
		"dataDir":       n.DataDir,
	})
}

func (n *Node) handleList(w http.ResponseWriter, r *http.Request) {
	type fileEntry struct {
		FileID string `json:"fileId"`
		Size   int64  `json:"size"`
	}
	var files []fileEntry

	// Walk through data directory
	filepath.Walk(n.DataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		fileID := filepath.Base(path)
		files = append(files, fileEntry{FileID: fileID, Size: info.Size()})
		return nil
	})

	writeJSON(w, map[string]any{"files": files, "count": len(files)})
}
func (n *Node) handleDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FileID string `json:"fileId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.FileID == "" {
		http.Error(w, "bad json", 400)
		return
	}
	path := n.dataPathFor(body.FileID)
	info, err := os.Stat(path)
	if err != nil {
		writeJSON(w, map[string]any{"deleted": false, "exists": false})
		return
	}
	_ = os.Remove(path)
	if info != nil {
		n.addUsed(-info.Size())
	}
	writeJSON(w, map[string]any{"deleted": true})
}

func (n *Node) handleVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FileID   string `json:"fileId"`
		Checksum string `json:"checksum"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", 400)
		return
	}

	path := n.dataPathFor(body.FileID)
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "file not found", 404)
		return
	}
	defer f.Close()

	h := sha256.New()
	io.Copy(h, f)
	computedChecksum := "sha256:" + hex.EncodeToString(h.Sum(nil))

	matches := computedChecksum == body.Checksum
	writeJSON(w, map[string]any{
		"fileId":           body.FileID,
		"expectedChecksum": body.Checksum,
		"actualChecksum":   computedChecksum,
		"verified":         matches,
	})
}

func (n *Node) handleShutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true})
	go func() { time.Sleep(200 * time.Millisecond); os.Exit(0) }()
}

func (n *Node) registerToNaming() {
	body := map[string]any{"nodeId": n.NodeID, "url": fmt.Sprintf("http://localhost:%s", n.Port), "capacityBytes": n.CapacityBytes}
	_ = postJSON(n.NamingURL+"/register-node", body)
}
func (n *Node) startHeartbeat() {
	t := time.NewTicker(5 * time.Second)
	go func() {
		for range t.C {
			_ = postJSON(n.NamingURL+"/heartbeat", map[string]any{"nodeId": n.NodeID, "usedBytes": n.currentUsed()})
		}
	}()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func postJSON(url string, body any) error {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)
	return nil
}

func main() {
	node := &Node{
		NodeID:        getenv("NODE_ID", "node-a"),
		Port:          getenv("PORT", "9001"),
		DataDir:       getenv("DATA_DIR", "./data"),
		NamingURL:     getenv("NAMING_URL", "http://localhost:8000"),
		CapacityBytes: 1 << 30,
	}
	if v := getenv("CAPACITY_BYTES", ""); v != "" {
		var x int64
		fmt.Sscanf(v, "%d", &x)
		if x > 0 {
			node.CapacityBytes = x
		}
	}
	_ = os.MkdirAll(node.DataDir, 0755)

	mux := http.NewServeMux()
	mux.HandleFunc("/upload", node.handleUpload)
	mux.HandleFunc("/download/", node.handleDownload)
	mux.HandleFunc("/has", node.handleHas)
	mux.HandleFunc("/health", node.handleHealth)
	mux.HandleFunc("/list", node.handleList)
	mux.HandleFunc("/verify", node.handleVerify)
	mux.HandleFunc("/shutdown", node.handleShutdown)
	mux.HandleFunc("/delete", node.handleDelete)

	node.registerToNaming()
	node.startHeartbeat()

	addr := ":" + node.Port
	log.Printf("Storage Node %s at %s (data=%s)", node.NodeID, addr, node.DataDir)
	log.Fatal(http.ListenAndServe(addr, mux))
}
