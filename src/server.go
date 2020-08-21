package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"text/template"
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

type Variables struct {
	HtmlTitle string
	Title     string
	Message   string
	Image     string
	Facebook  string
	Twitter   string
	Youtube   string
}

func getEnvOrDefault(env string, def string) string {
	if os.Getenv(env) == "" {
		return def
	}
	return os.Getenv(env)
}

func parseIndex() {
	index := "/static/index.html"

	if _, err := os.Stat(index); err == nil {

		// Get the environment variables
		environment := Variables{
			getEnvOrDefault("HTML_TITLE", "Coming soon"),
			getEnvOrDefault("TITLE", "soon"),
			getEnvOrDefault("MESSAGE", "Our website is <span class=\"m1-txt2\">Coming Soon</span>, follow us for update now!"),
			os.Getenv("BG_IMAGE"),
			os.Getenv("FACEBOOK"),
			os.Getenv("TWITTER"),
			os.Getenv("YOUTUBE")}

		// Read entire index content
		content, err := ioutil.ReadFile(index)
		if err != nil {
			panic(err)
		}
		text := string(content)

		// Save back
		f, err := os.Create(index)
		if err != nil {
			panic(err)
		}

		// Template
		tmpl, err := template.New("index").Parse(text)
		if err != nil {
			panic(err)
		}
		err = tmpl.Execute(f, environment)
		if err != nil {
			panic(err)
		}
	}
}

func main() {
	http.HandleFunc("/", wrapHandler(
		http.FileServer(http.Dir("/static"))))

	parseIndex()

	// Start server
	log.Printf("byjg/static-httpserver")
	log.Printf("Listen on 8080")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}
