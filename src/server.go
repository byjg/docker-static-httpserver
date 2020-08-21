package main

import (
    "log"
    "net/http"
)

type StatusRespWr struct {
    http.ResponseWriter // We embed http.ResponseWriter
    status              int
}

func (w *StatusRespWr) WriteHeader(status int) {
    w.status = status // Store the status for our own use
    w.ResponseWriter.WriteHeader(status)
}

func wrapHandler(h http.Handler) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        srw := &StatusRespWr{ResponseWriter: w}

        h.ServeHTTP(srw, r)

        log.Printf("%s - \"%s %s %s\" %d %d \"%s\"", r.RemoteAddr, r.Method, r.RequestURI,
            r.Proto, srw.status, r.ContentLength, r.UserAgent())
    }
}

func main() {
    http.HandleFunc("/", wrapHandler(
        http.FileServer(http.Dir("/static"))))

    log.Printf("byjg/static-httpserver")
    log.Printf("Listen on 8080")

    if err := http.ListenAndServe(":8080", nil); err != nil {
        panic(err)
    }
}
