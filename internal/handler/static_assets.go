package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// staticETags maps each embedded asset path to a hash of its contents.
//
// Files inside an embed.FS carry a zero modification time, so net/http emits
// neither Last-Modified nor ETag for them. Browsers then fall back to heuristic
// caching and can serve a stale stylesheet long after a deploy. Hashing the
// bytes once at startup gives every asset a real validator.
var staticETags = buildStaticETags()

func buildStaticETags() map[string]string {
	etags := make(map[string]string)
	_ = fs.WalkDir(assetFiles, "static", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		content, readErr := assetFiles.ReadFile(name)
		if readErr != nil {
			return nil
		}
		sum := sha256.Sum256(content)
		etags["/"+name] = `"` + hex.EncodeToString(sum[:16]) + `"`
		return nil
	})
	return etags
}

// staticHandler serves the embedded assets with a validator attached.
// "no-cache" does not mean "do not cache" - it means revalidate before reuse,
// so repeat visits still get a cheap 304 while a deploy is picked up at once.
func staticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(assetFiles))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(r.URL.Path)
		etag, ok := staticETags[clean]
		if !ok {
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if matchesETag(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func matchesETag(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}
