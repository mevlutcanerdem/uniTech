package main

import (
	"sort"
	"time"
)

// Rapor zamanları kampüsün saat diliminde (UTC+3) gruplanır.
var campusTZ = time.FixedZone("TRT", 3*3600)

type Count struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type DayCount struct {
	Date     string `json:"date"` // 2026-10-23
	Created  int    `json:"created"`
	Resolved int    `json:"resolved"`
}

type StaffStat struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Open     int    `json:"open"`
	Resolved int    `json:"resolved"`
}

type Report struct {
	Totals          Stats       `json:"totals"`
	Total           int         `json:"total"`
	Unassigned      int         `json:"unassigned"`
	OpenOver48h     int         `json:"open_over_48h"`
	AvgResolveHours float64     `json:"avg_resolve_hours"`
	ByCategory      []Count     `json:"by_category"`
	ByBuilding      []Count     `json:"by_building"`
	Daily           []DayCount  `json:"daily"`
	Staff           []StaffStat `json:"staff"`
}

func sortedCounts(m map[string]int) []Count {
	out := make([]Count, 0, len(m))
	for k, v := range m {
		out = append(out, Count{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Report son `days` günün özetini çıkarır.
func (s *Store) Report(days int, now time.Time) Report {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := Report{}
	cat, bld := map[string]int{}, map[string]int{}
	staff := map[int]*StaffStat{}
	for _, u := range s.d.Users {
		if u.IsStaff() {
			staff[u.ID] = &StaffStat{ID: u.ID, Name: u.Name}
		}
	}
	today := now.In(campusTZ)
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, campusTZ).AddDate(0, 0, -(days - 1))
	daily := make([]DayCount, days)
	idx := map[string]int{}
	for i := range daily {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		daily[i].Date = d
		idx[d] = i
	}

	var resolveSum float64
	var resolved int
	for _, t := range s.d.Tickets {
		r.Total++
		cat[t.Category]++
		bld[t.Building]++
		switch t.Status {
		case StatusNew:
			r.Totals.New++
		case StatusProgress:
			r.Totals.Progress++
		case StatusDone:
			r.Totals.Done++
		}
		open := t.Status != StatusDone
		if open && t.Priority == "acil" {
			r.Totals.Urgent++
		}
		if open && t.AssigneeID == 0 {
			r.Unassigned++
		}
		if open && now.Sub(t.CreatedAt) > 48*time.Hour {
			r.OpenOver48h++
		}
		if i, ok := idx[t.CreatedAt.In(campusTZ).Format("2006-01-02")]; ok {
			daily[i].Created++
		}
		if t.ResolvedAt != nil {
			resolveSum += t.ResolvedAt.Sub(t.CreatedAt).Hours()
			resolved++
			if i, ok := idx[t.ResolvedAt.In(campusTZ).Format("2006-01-02")]; ok {
				daily[i].Resolved++
			}
		}
		if st, ok := staff[t.AssigneeID]; ok {
			if open {
				st.Open++
			} else {
				st.Resolved++
			}
		}
	}
	if resolved > 0 {
		r.AvgResolveHours = resolveSum / float64(resolved)
	}
	r.ByCategory = sortedCounts(cat)
	r.ByBuilding = sortedCounts(bld)
	r.Daily = daily
	for _, st := range staff {
		r.Staff = append(r.Staff, *st)
	}
	sort.Slice(r.Staff, func(i, j int) bool {
		if r.Staff[i].Resolved != r.Staff[j].Resolved {
			return r.Staff[i].Resolved > r.Staff[j].Resolved
		}
		return r.Staff[i].Name < r.Staff[j].Name
	})
	return r
}
