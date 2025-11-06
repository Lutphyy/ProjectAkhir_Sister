package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

/* ==================== TYPES ==================== */

type ReplicaStatus string
const (
	ReplicaReady   ReplicaStatus = "READY"
	ReplicaMissing ReplicaStatus = "MISSING"
	ReplicaStale   ReplicaStatus = "STALE"
)

type FileState string
const (
	StateAllocated FileState = "ALLOCATED"
	StatePartial   FileState = "PARTIAL"
	StateAvailable FileState = "AVAILABLE"
	StateDegraded  FileState = "DEGRADED"
	StateDeleted   FileState = "DELETED"
)

type NodeStatus string
const (
	NodeHealthy NodeStatus = "HEALTHY"
	NodeSuspect NodeStatus = "SUSPECT"
	NodeDown    NodeStatus = "DOWN"
)

type ReplicaInfo struct {
	NodeID         string        `json:"nodeId"`
	URL            string        `json:"url"`
	Status         ReplicaStatus `json:"status"`
	LastVerifiedAt time.Time     `json:"lastVerifiedAt"`
}

type FileMetadata struct {
	FileID      string        `json:"fileId"`
	Filename    string        `json:"filename"`
	Size        int64         `json:"size"`
	Checksum    string        `json:"checksum"`
	ContentType string        `json:"contentType"`
	Version     int           `json:"version"`
	Replicas    []ReplicaInfo `json:"replicas"`
	State       FileState     `json:"state"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

type NodeInfo struct {
	NodeID        string     `json:"nodeId"`
	URL           string     `json:"url"`
	CapacityBytes int64      `json:"capacityBytes"`
	UsedBytes     int64      `json:"usedBytes"`
	Status        NodeStatus `json:"status"`
	LastSeenAt    time.Time  `json:"lastSeenAt"`
	Zone          string     `json:"zone,omitempty"`
	Tags          []string   `json:"tags,omitempty"`
	LastChosen    time.Time  `json:"lastChosen"`
}

/* ============== IN-MEM STORE + PERSIST ============== */

type Store struct {
	mu        sync.RWMutex
	files     map[string]*FileMetadata // fileId -> meta
	nodes     map[string]*NodeInfo     // nodeId -> info
	filesPath string
	nodesPath string
	repFactor int
}

func NewStore(base string, repFactor int) (*Store, error) {
	if err := os.MkdirAll(base, 0755); err != nil {
		return nil, err
	}
	s := &Store{
		files:     map[string]*FileMetadata{},
		nodes:     map[string]*NodeInfo{},
		filesPath: filepath.Join(base, "files.json"),
		nodesPath: filepath.Join(base, "nodes.json"),
		repFactor: repFactor,
	}
	_ = s.load()
	return s, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, err := os.ReadFile(s.filesPath); err == nil {
		_ = json.Unmarshal(b, &s.files)
	}
	if b, err := os.ReadFile(s.nodesPath); err == nil {
		_ = json.Unmarshal(b, &s.nodes)
	}
	return nil
}

func (s *Store) persist() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_ = writeJSONFile(s.filesPath, s.files)
	_ = writeJSONFile(s.nodesPath, s.nodes)
}

func writeJSONFile(path string, v any) error {
	tmp := path + ".tmp"
	b, _ := json.MarshalIndent(v, "", "  ")
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

/* ==================== HELPERS ==================== */

func now() time.Time { return time.Now().UTC() }

func healthOf(n *NodeInfo) NodeStatus {
	ago := time.Since(n.LastSeenAt)
	switch {
	case ago > 20*time.Second:
		return NodeDown
	case ago > 10*time.Second:
		return NodeSuspect
	default:
		return NodeHealthy
	}
}

func freeBytes(n *NodeInfo) int64 { return n.CapacityBytes - n.UsedBytes }

func loadFactor(n *NodeInfo) float64 {
	if n.CapacityBytes <= 0 {
		return math.MaxFloat64
	}
	return float64(n.UsedBytes) / float64(n.CapacityBytes)
}

func uuidLike(seed string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", seed, time.Now().UnixNano())))
	hexed := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexed[:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32])
}

/* ==================== HTTP SERVER ==================== */

type Server struct{ store *Store }

func (sv *Server) handleRegisterNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID        string   `json:"nodeId"`
		URL           string   `json:"url"`
		CapacityBytes int64    `json:"capacityBytes"`
		Zone          string   `json:"zone,omitempty"`
		Tags          []string `json:"tags,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		body.NodeID == "" || body.URL == "" || body.CapacityBytes <= 0 {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	sv.store.mu.Lock()
	sv.store.nodes[body.NodeID] = &NodeInfo{
		NodeID:        body.NodeID,
		URL:           body.URL,
		CapacityBytes: body.CapacityBytes,
		UsedBytes:     0,
		Status:        NodeHealthy,
		LastSeenAt:    now(),
		Zone:          body.Zone,
		Tags:          body.Tags,
	}
	sv.store.mu.Unlock()
	go sv.store.persist()

	writeJSONResp(w, map[string]any{"ok": true})
}

func (sv *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID    string `json:"nodeId"`
		UsedBytes int64  `json:"usedBytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	sv.store.mu.Lock()
	defer sv.store.mu.Unlock()
	n, ok := sv.store.nodes[body.NodeID]
	if !ok {
		http.Error(w, "unknown node", http.StatusNotFound)
		return
	}
	n.UsedBytes = body.UsedBytes
	n.LastSeenAt = now()
	n.Status = healthOf(n)
	go sv.store.persist()

	writeJSONResp(w, map[string]any{"ok": true, "status": n.Status})
}

func (sv *Server) handleAllocate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Filename    string `json:"filename"`
		Size        int64  `json:"size"`
		Checksum    string `json:"checksum"`
		ContentType string `json:"contentType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		body.Filename == "" || body.Size <= 0 || !strings.HasPrefix(body.Checksum, "sha256:") {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	fileID := uuidLike(body.Filename)
	replicas, err := sv.pickReplicas(body.Size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	meta := &FileMetadata{
		FileID:      fileID,
		Filename:    body.Filename,
		Size:        body.Size,
		Checksum:    body.Checksum,
		ContentType: body.ContentType,
		Version:     1,
		State:       StateAllocated,
		CreatedAt:   now(),
		UpdatedAt:   now(),
	}
	for _, n := range replicas {
		meta.Replicas = append(meta.Replicas, ReplicaInfo{
			NodeID: n.NodeID, URL: n.URL, Status: ReplicaReady, LastVerifiedAt: now(),
		})
	}

	sv.store.mu.Lock()
	sv.store.files[fileID] = meta
	for _, n := range replicas {
		sv.store.nodes[n.NodeID].LastChosen = now()
	}
	sv.store.mu.Unlock()
	go sv.store.persist()

	type outRep struct{ NodeID, URL string }
	out := struct {
		FileID   string   `json:"fileId"`
		Replicas []outRep `json:"replicas"`
	}{FileID: fileID}
	for _, rinfo := range meta.Replicas {
		out.Replicas = append(out.Replicas, outRep{rinfo.NodeID, rinfo.URL})
	}
	writeJSONResp(w, out)
}

func (sv *Server) pickReplicas(size int64) ([]*NodeInfo, error) {
	sv.store.mu.RLock()
	defer sv.store.mu.RUnlock()

	var cands []*NodeInfo
	for _, n := range sv.store.nodes {
		if healthOf(n) == NodeHealthy && freeBytes(n) >= size {
			cands = append(cands, n)
		}
	}
	if len(cands) < sv.store.repFactor {
		return nil, errors.New("insufficient healthy nodes")
	}
	sort.Slice(cands, func(i, j int) bool {
		li, lj := loadFactor(cands[i]), loadFactor(cands[j])
		if li == lj {
			return cands[i].LastChosen.Before(cands[j].LastChosen)
		}
		return li < lj
	})
	return cands[:sv.store.repFactor], nil
}

func (sv *Server) handleCommit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FileID   string   `json:"fileId"`
		Uploaded []string `json:"uploaded"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	sv.store.mu.Lock()
	defer sv.store.mu.Unlock()
	meta, ok := sv.store.files[body.FileID]
	if !ok {
		http.Error(w, "fileId not found", http.StatusNotFound)
		return
	}

	uploaded := map[string]bool{}
	for _, id := range body.Uploaded {
		uploaded[id] = true
	}
	count := 0
	for i := range meta.Replicas {
		if uploaded[meta.Replicas[i].NodeID] {
			count++
			meta.Replicas[i].Status = ReplicaReady
			meta.Replicas[i].LastVerifiedAt = now()
		}
	}
	switch {
	case count == 0:
		meta.State = StateAllocated
	case count < sv.store.repFactor:
		meta.State = StatePartial
	default:
		meta.State = StateAvailable
	}
	meta.UpdatedAt = now()
	go sv.store.persist()

	writeJSONResp(w, map[string]any{"state": meta.State})
}

func (sv *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/lookup/")
	if fileID == "" {
		http.Error(w, "missing fileId", http.StatusBadRequest)
		return
	}
	sv.store.mu.RLock()
	meta, ok := sv.store.files[fileID]
	sv.store.mu.RUnlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	type out struct{ NodeID, URL string }
	var healthy, others []out

	sv.store.mu.RLock()
	for _, rep := range meta.Replicas {
		n := sv.store.nodes[rep.NodeID]
		if healthOf(n) == NodeHealthy {
			healthy = append(healthy, out{rep.NodeID, rep.URL})
		} else {
			others = append(others, out{rep.NodeID, rep.URL})
		}
	}
	sv.store.mu.RUnlock()

	writeJSONResp(w, append(healthy, others...))
}

func (sv *Server) handleReportMissing(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FileID string `json:"fileId"`
		NodeID string `json:"nodeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	sv.store.mu.Lock()
	defer sv.store.mu.Unlock()
	meta, ok := sv.store.files[body.FileID]
	if !ok {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	missing := 0
	for i := range meta.Replicas {
		if meta.Replicas[i].NodeID == body.NodeID {
			meta.Replicas[i].Status = ReplicaMissing
		}
		if meta.Replicas[i].Status != ReplicaReady {
			missing++
		}
	}
	if missing > 0 && meta.State == StateAvailable {
		meta.State = StateDegraded
	}
	meta.UpdatedAt = now()
	go sv.store.persist()

	writeJSONResp(w, map[string]any{"accepted": true, "state": meta.State})
}

/* ============== SHARED RESP & BOOTSTRAP ============== */

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func logRequest(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func main() {
	store, err := NewStore("metadata", 2) // replication factor = 2
	if err != nil {
		log.Fatal(err)
	}

	sv := &Server{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/register-node",  sv.handleRegisterNode)
	mux.HandleFunc("/heartbeat",      sv.handleHeartbeat)
	mux.HandleFunc("/allocate",       sv.handleAllocate)
	mux.HandleFunc("/commit",         sv.handleCommit)
	mux.HandleFunc("/lookup/",        sv.handleLookup)        // /lookup/{fileId}
	mux.HandleFunc("/report-missing", sv.handleReportMissing)

	addr := ":8000"
	log.Printf("Naming Service running at %s ...", addr)
	log.Fatal(http.ListenAndServe(addr, logRequest(mux)))
}
