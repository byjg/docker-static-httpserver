package main

import (
	"bytes"
	"container/list"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
)

// --- Configuration ---

type config struct {
	port             string
	spaMode          bool
	rootDir          string
	cacheMaxSize     int64
	cacheMaxFileSize int64
}

func loadConfig() config {
	return config{
		port:             getEnvOrDefault("PORT", "8080"),
		spaMode:          parseBool(os.Getenv("SPA_MODE")),
		rootDir:          "/static",
		cacheMaxSize:     parseInt64(getEnvOrDefault("CACHE_MAX_SIZE", "50000000")),
		cacheMaxFileSize: parseInt64(getEnvOrDefault("CACHE_MAX_FILE_SIZE", "5000000")),
	}
}

func getEnvOrDefault(env string, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes"
}

func parseInt64(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// --- Template Variables ---

type Variables struct {
	HtmlTitle string
	Title     string
	Message   string
	Image     string
	Facebook  string
	Twitter   string
	Youtube   string
}

func loadVariables() Variables {
	return Variables{
		HtmlTitle: getEnvOrDefault("HTML_TITLE", "Coming soon"),
		Title:     getEnvOrDefault("TITLE", "soon"),
		Message:   getEnvOrDefault("MESSAGE", "Our website is <span class=\"m1-txt2\">Coming Soon</span>, follow us for update now!"),
		Image:     os.Getenv("BG_IMAGE"),
		Facebook:  os.Getenv("FACEBOOK"),
		Twitter:   os.Getenv("TWITTER"),
		Youtube:   os.Getenv("YOUTUBE"),
	}
}

// --- LRU File Cache ---

var errNotFound = errors.New("file not found")

type cacheEntry struct {
	key         string
	content     []byte
	contentType string
	pinned      bool
}

type fileCache struct {
	mu          sync.Mutex
	entries     map[string]*list.Element
	lruList     *list.List
	currentSize int64
	maxSize     int64
	maxFileSize int64
	rootDir     string
	disabled    bool
}

func newFileCache(cfg config) *fileCache {
	return &fileCache{
		entries:     make(map[string]*list.Element),
		lruList:     list.New(),
		maxSize:     cfg.cacheMaxSize,
		maxFileSize: cfg.cacheMaxFileSize,
		rootDir:     cfg.rootDir,
		disabled:    cfg.cacheMaxSize == 0,
	}
}

func (fc *fileCache) seedIndex(vars Variables) error {
	index := filepath.Join(fc.rootDir, "index.html")

	if _, err := os.Stat(index); err != nil {
		return nil // index.html doesn't exist, not an error
	}

	content, err := os.ReadFile(index)
	if err != nil {
		return fmt.Errorf("reading index.html: %w", err)
	}

	tmpl, err := template.New("index").Parse(string(content))
	if err != nil {
		return fmt.Errorf("parsing index.html template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return fmt.Errorf("executing index.html template: %w", err)
	}

	entry := &cacheEntry{
		key:         "/index.html",
		content:     buf.Bytes(),
		contentType: "text/html; charset=utf-8",
		pinned:      true,
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	elem := fc.lruList.PushFront(entry)
	fc.entries["/index.html"] = elem
	fc.entries["/"] = elem // "/" resolves to index.html
	fc.currentSize += int64(len(entry.content))

	return nil
}

func (fc *fileCache) get(urlPath string) (*cacheEntry, error) {
	fc.mu.Lock()

	// Check cache
	if elem, ok := fc.entries[urlPath]; ok {
		fc.lruList.MoveToFront(elem)
		entry := elem.Value.(*cacheEntry)
		fc.mu.Unlock()
		return entry, nil
	}

	fc.mu.Unlock()

	// Resolve filesystem path
	entry, err := fc.readFromDisk(urlPath)
	if err != nil {
		return nil, err
	}

	// Skip caching if disabled or file too large
	if fc.disabled || int64(len(entry.content)) > fc.maxFileSize {
		return entry, nil
	}

	// Insert into cache with eviction
	fc.mu.Lock()
	defer fc.mu.Unlock()

	// Double-check: another goroutine may have cached it
	if elem, ok := fc.entries[urlPath]; ok {
		fc.lruList.MoveToFront(elem)
		return elem.Value.(*cacheEntry), nil
	}

	entrySize := int64(len(entry.content))

	// Evict LRU entries until there's room
	for fc.currentSize+entrySize > fc.maxSize && fc.lruList.Len() > 0 {
		back := fc.lruList.Back()
		if back == nil {
			break
		}
		victim := back.Value.(*cacheEntry)
		if victim.pinned {
			// Move pinned entry before the back so we can try the next one
			// If only pinned entries remain, stop evicting
			if fc.lruList.Len() <= 1 {
				break
			}
			// Find next non-pinned from back
			evicted := false
			for e := fc.lruList.Back(); e != nil; e = e.Prev() {
				v := e.Value.(*cacheEntry)
				if !v.pinned {
					fc.currentSize -= int64(len(v.content))
					delete(fc.entries, v.key)
					fc.lruList.Remove(e)
					evicted = true
					break
				}
			}
			if !evicted {
				break // only pinned entries left
			}
			continue
		}
		fc.currentSize -= int64(len(victim.content))
		delete(fc.entries, victim.key)
		fc.lruList.Remove(back)
	}

	// If still no room, serve without caching
	if fc.currentSize+entrySize > fc.maxSize {
		return entry, nil
	}

	elem := fc.lruList.PushFront(entry)
	fc.entries[urlPath] = elem
	fc.currentSize += entrySize

	return entry, nil
}

func (fc *fileCache) readFromDisk(urlPath string) (*cacheEntry, error) {
	clean := filepath.Clean(urlPath)
	fsPath := filepath.Join(fc.rootDir, clean)

	// Security: prevent directory traversal
	absRoot, _ := filepath.Abs(fc.rootDir)
	absPath, _ := filepath.Abs(fsPath)
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return nil, errNotFound
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		return nil, errNotFound
	}

	// If directory, try index.html inside it
	if info.IsDir() {
		fsPath = filepath.Join(fsPath, "index.html")
		if _, err := os.Stat(fsPath); err != nil {
			return nil, errNotFound
		}
	}

	content, err := os.ReadFile(fsPath)
	if err != nil {
		return nil, errNotFound
	}

	ct := mime.TypeByExtension(filepath.Ext(fsPath))
	if ct == "" {
		ct = http.DetectContentType(content)
	}

	return &cacheEntry{
		key:         urlPath,
		content:     content,
		contentType: ct,
	}, nil
}

// --- HTTP Handlers ---

type statusRespWr struct {
	http.ResponseWriter
	status int
}

func (w *statusRespWr) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func wrapHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srw := &statusRespWr{ResponseWriter: w, status: 200}
		h(srw, r)
		log.Printf("%s - \"%s %s %s\" %d %d \"%s\"",
			r.RemoteAddr, r.Method, r.RequestURI, r.Proto,
			srw.status, r.ContentLength, r.UserAgent())
	}
}

func serveStatic(cache *fileCache, cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Health endpoint
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}

		// Try to serve the requested file
		entry, err := cache.get(r.URL.Path)
		if err == nil {
			w.Header().Set("Content-Type", entry.contentType)
			w.Header().Set("Content-Length", strconv.Itoa(len(entry.content)))
			w.WriteHeader(http.StatusOK)
			w.Write(entry.content)
			return
		}

		// SPA fallback: serve index.html for paths without file extension
		if cfg.spaMode && filepath.Ext(r.URL.Path) == "" {
			entry, err := cache.get("/index.html")
			if err == nil {
				w.Header().Set("Content-Type", entry.contentType)
				w.Header().Set("Content-Length", strconv.Itoa(len(entry.content)))
				w.WriteHeader(http.StatusOK)
				w.Write(entry.content)
				return
			}
		}

		http.NotFound(w, r)
	}
}

// --- Main ---

func main() {
	cfg := loadConfig()
	cache := newFileCache(cfg)

	vars := loadVariables()
	if err := cache.seedIndex(vars); err != nil {
		log.Fatalf("Failed to process index template: %v", err)
	}

	http.HandleFunc("/", wrapHandler(serveStatic(cache, cfg)))

	log.Printf("byjg/static-httpserver")
	log.Printf("Listen on %s", cfg.port)
	if cfg.spaMode {
		log.Printf("SPA mode enabled")
	}
	if cache.disabled {
		log.Printf("Cache disabled")
	} else {
		log.Printf("Cache max size: %d bytes, max file size: %d bytes", cache.maxSize, cache.maxFileSize)
	}

	srv := &http.Server{
		Addr:         ":" + cfg.port,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}
