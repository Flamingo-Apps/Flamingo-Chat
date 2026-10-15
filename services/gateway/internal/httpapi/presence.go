package httpapi

import (
	"net/http"

	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
)

// handlePresence is deliberately not behind withAuth - presence counts
// identify no one (docs/ARCHITECTURE.md §3.4) and the client needs them
// before signing in to show the landing page's live count.
func (s *Server) handlePresence(w http.ResponseWriter, r *http.Request) {
	resp, err := s.presence.GetLiveCounts(r.Context(), &presencev1.GetLiveCountsRequest{})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Total  int64 `json:"total"`
		Guests int64 `json:"guests"`
		Male   int64 `json:"male"`
		Female int64 `json:"female"`
	}{resp.GetTotal(), resp.GetGuests(), resp.GetMale(), resp.GetFemale()})
}
