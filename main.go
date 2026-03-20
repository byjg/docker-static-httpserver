package main

import (
	"bytes"
	"container/list"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
)

var version = "dev"

// --- Configuration ---

type config struct {
	port             string
	tlsPort          string
	tlsCertDir       string
	spaMode          bool
	showHeaders      bool
	rootDir          string
	cacheMaxSize     int64
	cacheMaxFileSize int64
}

func flagOrEnvStr(flagVal string, envName string, def string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv(envName); v != "" {
		return v
	}
	return def
}

func flagOrEnvBool(flagVal bool, envName string) bool {
	if flagVal {
		return true
	}
	return parseBool(os.Getenv(envName))
}

func flagOrEnvInt64(flagVal int64, envName string, def int64) int64 {
	if flagVal >= 0 {
		return flagVal
	}
	if v := os.Getenv(envName); v != "" {
		return parseInt64(v)
	}
	return def
}

func loadConfig() config {
	var (
		fPort         string
		fTlsPort      string
		fTlsCertDir   string
		fSpa          bool
		fShowHeaders  bool
		fRootDir      string
		fCacheMax     int64
		fCacheMaxFile int64
		fVersion      bool
	)

	flag.StringVar(&fPort, "port", "", "HTTP listening port (env: PORT, default: 8080)")
	flag.StringVar(&fTlsPort, "tls-port", "", "HTTPS listening port (env: TLS_PORT, default: 8443)")
	flag.StringVar(&fTlsCertDir, "tls-cert-dir", "", "TLS certificate directory (env: TLS_CERT_DIR, default: /certs)")
	flag.BoolVar(&fSpa, "spa", false, "Enable SPA mode (env: SPA_MODE)")
	flag.BoolVar(&fShowHeaders, "show-headers", false, "Show request headers on parking page (env: SHOW_HEADERS)")
	flag.StringVar(&fRootDir, "root-dir", "", "Root directory for static files (env: ROOT_DIR, default: /static)")
	flag.Int64Var(&fCacheMax, "cache-max-size", -1, "Max cache size in bytes, 0 to disable (env: CACHE_MAX_SIZE, default: 50000000)")
	flag.Int64Var(&fCacheMaxFile, "cache-max-file", -1, "Max file size to cache in bytes (env: CACHE_MAX_FILE_SIZE, default: 5000000)")
	flag.BoolVar(&fVersion, "version", false, "Print version and exit")
	flag.Parse()

	if fVersion {
		fmt.Printf("static-httpserver %s\n", version)
		os.Exit(0)
	}

	return config{
		port:             flagOrEnvStr(fPort, "PORT", "8080"),
		tlsPort:          flagOrEnvStr(fTlsPort, "TLS_PORT", "8443"),
		tlsCertDir:       flagOrEnvStr(fTlsCertDir, "TLS_CERT_DIR", "/certs"),
		spaMode:          flagOrEnvBool(fSpa, "SPA_MODE"),
		showHeaders:      flagOrEnvBool(fShowHeaders, "SHOW_HEADERS"),
		rootDir:          flagOrEnvStr(fRootDir, "ROOT_DIR", "/static"),
		cacheMaxSize:     flagOrEnvInt64(fCacheMax, "CACHE_MAX_SIZE", 50000000),
		cacheMaxFileSize: flagOrEnvInt64(fCacheMaxFile, "CACHE_MAX_FILE_SIZE", 5000000),
	}
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

// --- TLS ---

func loadOrGenerateTLS(certDir string) (tls.Certificate, error) {
	certFile := filepath.Join(certDir, "cert.pem")
	keyFile := filepath.Join(certDir, "key.pem")

	if _, err := os.Stat(certFile); err == nil {
		if _, err := os.Stat(keyFile); err == nil {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return tls.Certificate{}, fmt.Errorf("loading certificates from %s: %w", certDir, err)
			}
			log.Printf("TLS using certificates from %s", certDir)
			return cert, nil
		}
	}

	log.Printf("TLS using self-signed certificate")
	return generateSelfSignedCert()
}

func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generating private key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generating serial number: %w", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Static HTTP Server"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("creating certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshaling private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return tls.X509KeyPair(certPEM, keyPEM)
}

// --- Template Variables ---

type Variables struct {
	HtmlTitle   string
	Title       string
	Message     string
	Image       string
	Facebook    string
	Twitter     string
	Youtube     string
	ShowHeaders bool
}

func loadVariables(cfg config) Variables {
	return Variables{
		HtmlTitle:   getEnvOrDefault("HTML_TITLE", "Coming soon"),
		Title:       getEnvOrDefault("TITLE", "soon"),
		Message:     getEnvOrDefault("MESSAGE", "Our website is coming soon, follow us for updates!"),
		Image:       os.Getenv("BG_IMAGE"),
		Facebook:    os.Getenv("FACEBOOK"),
		Twitter:     os.Getenv("TWITTER"),
		Youtube:     os.Getenv("YOUTUBE"),
		ShowHeaders: cfg.showHeaders,
	}
}

func getEnvOrDefault(env string, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
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
		return nil
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
	fc.entries["/"] = elem
	fc.currentSize += int64(len(entry.content))

	return nil
}

func (fc *fileCache) get(urlPath string) (*cacheEntry, error) {
	fc.mu.Lock()

	if elem, ok := fc.entries[urlPath]; ok {
		fc.lruList.MoveToFront(elem)
		entry := elem.Value.(*cacheEntry)
		fc.mu.Unlock()
		return entry, nil
	}

	fc.mu.Unlock()

	entry, err := fc.readFromDisk(urlPath)
	if err != nil {
		return nil, err
	}

	if fc.disabled || int64(len(entry.content)) > fc.maxFileSize {
		return entry, nil
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	if elem, ok := fc.entries[urlPath]; ok {
		fc.lruList.MoveToFront(elem)
		return elem.Value.(*cacheEntry), nil
	}

	entrySize := int64(len(entry.content))

	for fc.currentSize+entrySize > fc.maxSize && fc.lruList.Len() > 0 {
		back := fc.lruList.Back()
		if back == nil {
			break
		}
		victim := back.Value.(*cacheEntry)
		if victim.pinned {
			if fc.lruList.Len() <= 1 {
				break
			}
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
				break
			}
			continue
		}
		fc.currentSize -= int64(len(victim.content))
		delete(fc.entries, victim.key)
		fc.lruList.Remove(back)
	}

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

	absRoot, _ := filepath.Abs(fc.rootDir)
	absPath, _ := filepath.Abs(fsPath)
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return nil, errNotFound
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		return nil, errNotFound
	}

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
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}

		if r.URL.Path == "/api/headers" && cfg.showHeaders {
			headers := make(map[string]string)
			keys := make([]string, 0, len(r.Header))
			for k := range r.Header {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				headers[k] = strings.Join(r.Header[k], ", ")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(headers)
			return
		}

		entry, err := cache.get(r.URL.Path)
		if err == nil {
			w.Header().Set("Content-Type", entry.contentType)
			w.Header().Set("Content-Length", strconv.Itoa(len(entry.content)))
			w.WriteHeader(http.StatusOK)
			w.Write(entry.content)
			return
		}

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

	vars := loadVariables(cfg)
	if err := cache.seedIndex(vars); err != nil {
		log.Fatalf("Failed to process index template: %v", err)
	}

	handler := wrapHandler(serveStatic(cache, cfg))

	log.Printf("byjg/static-httpserver %s", version)
	if cfg.spaMode {
		log.Printf("SPA mode enabled")
	}
	if cache.disabled {
		log.Printf("Cache disabled")
	} else {
		log.Printf("Cache max size: %d bytes, max file size: %d bytes", cache.maxSize, cache.maxFileSize)
	}

	httpSrv := &http.Server{
		Addr:         ":" + cfg.port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("HTTP listening on %s", cfg.port)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	cert, err := loadOrGenerateTLS(cfg.tlsCertDir)
	if err != nil {
		log.Fatalf("TLS setup failed: %v", err)
	}

	tlsSrv := &http.Server{
		Addr:    ":" + cfg.tlsPort,
		Handler: handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("HTTPS listening on %s", cfg.tlsPort)
	log.Fatal(tlsSrv.ListenAndServeTLS("", ""))
}
