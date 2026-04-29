package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:webfs
var webfsFS embed.FS

// staticHandler returns an http.Handler that serves the embedded test web
// client at the /web/ path prefix. Files live in the embedded webfs/
// subtree. A future TypeScript pipeline can replace these hand-written
// files with build outputs without changing the embed wiring.
func staticHandler() http.Handler {
	sub, err := fs.Sub(webfsFS, "webfs")
	if err != nil {
		// embed roots are statically known; this should be impossible.
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.StripPrefix("/web/", fileServer)
}
