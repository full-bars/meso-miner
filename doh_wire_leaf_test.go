package connect

// writeDohWire extracted verbatim from upstream net_http_doh_test.go at
// 20b29f79 so the ported internal-DoH and udp-candidates tests compile.

import (
	"encoding/base64"
	"net/http"
	"net/netip"

	"golang.org/x/net/dns/dnsmessage"
)

func writeDohWire(w http.ResponseWriter, r *http.Request, records []netip.Addr, ttl uint32, nxdomain bool) {
	raw, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("dns"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var p dnsmessage.Parser
	header, err := p.Start(raw)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	q, err := p.Question()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	h := dnsmessage.Header{ID: header.ID, Response: true, RecursionAvailable: true}
	if nxdomain {
		h.RCode = dnsmessage.RCodeNameError
	}
	b := dnsmessage.NewBuilder(nil, h)
	b.StartQuestions()
	b.Question(q)
	b.StartAnswers()
	rh := dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: ttl}
	for _, ip := range records {
		switch {
		case q.Type == dnsmessage.TypeA && ip.Is4():
			b.AResource(rh, dnsmessage.AResource{A: ip.As4()})
		case q.Type == dnsmessage.TypeAAAA && ip.Is6() && !ip.Is4In6():
			b.AAAAResource(rh, dnsmessage.AAAAResource{AAAA: ip.As16()})
		}
	}
	resp, err := b.Finish()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/dns-message")
	w.Write(resp)
}
