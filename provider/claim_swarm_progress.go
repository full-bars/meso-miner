// The claim swarm's progress reader endpoint. The daemon family's
// claim_progress.go keeps the census and publication and left this reader to
// the swarm family, which owns the ClaimSwarm member map. Ported from sn
// 4ca8316e's miner/claim_progress.go verbatim.
package main

import (
	"encoding/json"
	"net/http"

	"github.com/urfoundation/sn/protocol"
)

func (self *ClaimSwarm) serveClaimProgress(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || request.Context().Err() != nil || len(request.URL.RawQuery) > 256 {
		http.Error(writer, "invalid claim observation request", http.StatusBadRequest)
		return
	}
	query := request.URL.Query()
	values, ok := query["id"]
	if !ok || len(query) != 1 || len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 128 {
		http.Error(writer, "invalid claim member", http.StatusBadRequest)
		return
	}
	id := values[0]
	self.stateLock.Lock()
	owner, known := self.progress[id]
	self.stateLock.Unlock()
	if !known {
		http.NotFound(writer, request)
		return
	}
	raw, active := owner.snapshot(request.Context())
	if raw == nil {
		raw, _ = json.Marshal(protocol.ClaimProgress{Schema: protocol.ClaimProgressSchema, Member: id, Status: "unknown"})
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	if !active {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
	_, _ = writer.Write(raw)
}
