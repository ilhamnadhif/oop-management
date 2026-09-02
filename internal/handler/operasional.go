package handler

import (
	"net/http"
)

// OperasionalPageData is the placeholder each Operasional page renders. It
// carries nothing of its own: the title and the sentence under it come from the
// sidebar, which is the one place those words are written.
type OperasionalPageData struct {
	ShellPageData
}

// operasionalPage returns the handler for one Operasional page. Each page has
// its own nav key rather than sharing one, so a position's access and a
// project's menu switch apply to that page alone - which is what will be needed
// once the pages hold anything, and costs nothing now.
func (s *Server) operasionalPage(navKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, user, sessionValue, ok := s.requireAccess(w, r, navKey)
		if !ok {
			return
		}
		s.render(w, "operasional_placeholder", OperasionalPageData{
			ShellPageData: s.shellData(user, sessionValue, navKey),
		}, http.StatusOK)
	}
}
