package frnorm

import (
	"regexp"
	"strconv"
	"strings"
)

// reHearingTime matches an "HH[:hH]MM[:SS]" hearing-time string.
// Group 1 = hours (1–2 digits), group 2 = minutes (0–2 digits, optional),
// group 3 = seconds (0–2 digits, optional).
//
// Accepts the following separators between hour and minute :
//
//	":" (canonical SQL form)
//	"h" / "H" (French locale form, e.g. "14h00", "9h30")
var reHearingTime = regexp.MustCompile(`^([0-2]?\d)\s*[hH:]\s*([0-5]?\d)?(?:\s*[:hH]\s*([0-5]?\d))?$`)

// reHearingTimeBareHour matches a bare hour with the French "h" suffix and
// no minutes ("14h", "9H").
var reHearingTimeBareHour = regexp.MustCompile(`^([0-2]?\d)\s*[hH]$`)

// NormalizeHearingTime converts a hearing-time string to the canonical
// "HH:MM:SS" SQL form used by the auctions table.
//
// Recognised inputs (whitespace and trailing annotations like
// "(heure de Paris)" are stripped first):
//
//	"14h00"      → "14:00:00"
//	"14h"        → "14:00:00"
//	"14H30"      → "14:30:00"
//	"9h30"       → "09:30:00"
//	"14:00"      → "14:00:00"
//	"14:00:00"   → "14:00:00" (passthrough)
//	"" / junk    → "" (don't store garbage)
//
// Out-of-range hours (>23) or unparseable input return "".
func NormalizeHearingTime(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Strip a trailing parenthesised annotation, e.g. "14h00 (heure de Paris)".
	if i := strings.Index(s, "("); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if s == "" {
		return ""
	}
	// Bare hour: "14h" / "9H".
	if m := reHearingTimeBareHour.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		if h > 23 {
			return ""
		}
		return formatHMS(h, 0, 0)
	}
	m := reHearingTime.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	h, _ := strconv.Atoi(m[1])
	if h > 23 {
		return ""
	}
	min := 0
	if m[2] != "" {
		min, _ = strconv.Atoi(m[2])
	}
	sec := 0
	if m[3] != "" {
		sec, _ = strconv.Atoi(m[3])
	}
	return formatHMS(h, min, sec)
}

func formatHMS(h, m, s int) string {
	var b strings.Builder
	b.Grow(8)
	if h < 10 {
		b.WriteByte('0')
	}
	b.WriteString(strconv.Itoa(h))
	b.WriteByte(':')
	if m < 10 {
		b.WriteByte('0')
	}
	b.WriteString(strconv.Itoa(m))
	b.WriteByte(':')
	if s < 10 {
		b.WriteByte('0')
	}
	b.WriteString(strconv.Itoa(s))
	return b.String()
}
