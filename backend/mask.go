package main

import "strings"

// maskName her kelimenin ilk harfini bırakır: "Mevlüt Can Erdem" -> "M*** C*** E***".
func maskName(name string) string {
	parts := strings.Fields(name)
	for i, p := range parts {
		r := []rune(p)
		parts[i] = strings.ToUpper(string(r[0])) + "***"
	}
	if len(parts) == 0 {
		return "Gizli"
	}
	return strings.Join(parts, " ")
}

// maskEmail adresteki adı gizler: "mevlut.erdem@agu.edu.tr" -> "m***.e***@agu.edu.tr".
func maskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok {
		return "***"
	}
	segs := strings.Split(local, ".")
	for i, s := range segs {
		if r := []rune(s); len(r) > 0 {
			segs[i] = string(r[0]) + "***"
		}
	}
	return strings.Join(segs, ".") + "@" + domain
}

// anonymize, arızayı bildiren öğrencinin adını ve e-postasını her yanıtta gizler.
// Tam bilgi veritabanında kalır; API hiçbir zaman dışarı vermez.
func anonymize(t Ticket) Ticket {
	t.ReporterName = maskName(t.ReporterName)
	t.ReporterEmail = maskEmail(t.ReporterEmail)
	h := make([]HistoryEntry, len(t.History))
	copy(h, t.History)
	for i := range h {
		if h[i].ByID == t.ReporterID {
			h[i].ByName = t.ReporterName
		}
	}
	t.History = h
	return t
}

func anonymizeAll(ts []Ticket) []Ticket {
	out := make([]Ticket, len(ts))
	for i, t := range ts {
		out[i] = anonymize(t)
	}
	return out
}
