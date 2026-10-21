package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// LogLine is one line of the tunnel's own log, trimmed to what is worth
// reading: when it happened, how loud it was, and what it said.
type LogLine struct {
	Seq   int64  `json:"seq"`
	Time  string `json:"time"`
	Level string `json:"level"`
	Text  string `json:"text"`
}

// LogBatch is what a page gets each time new lines arrive.
type LogBatch struct {
	Lines []LogLine `json:"lines"`
	Reset bool      `json:"reset"`
}

// logSink receives pingtunnel's log through loggo's printer and keeps a
// bounded ring of it, so the page can show a live session log without the
// tunnel writing files.
type logSink struct {
	mu       sync.Mutex
	lines    []LogLine
	seq      int64
	minLevel int
	muted    bool
	limit    int
	partial  string
}

var levelRank = map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}

func newLogSink() *logSink {
	return &logSink{minLevel: levelRank["info"], limit: 3000}
}

func (s *logSink) setFilter(level string, muted bool) {
	rank, ok := levelRank[strings.ToLower(strings.TrimSpace(level))]
	if !ok {
		rank = levelRank["info"]
	}
	s.mu.Lock()
	s.minLevel = rank
	s.muted = muted
	s.mu.Unlock()
}

func (s *logSink) clear() {
	s.mu.Lock()
	s.lines = nil
	s.mu.Unlock()
}

// Write never blocks the tunnel for long, and never loses a partial line.
func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.muted {
		return len(p), nil
	}
	s.partial += string(p)
	for {
		i := strings.IndexByte(s.partial, '\n')
		if i < 0 {
			break
		}
		raw := s.partial[:i]
		s.partial = s.partial[i+1:]
		s.appendLocked(raw)
	}
	if len(s.partial) > 8192 {
		s.partial = ""
	}
	return len(p), nil
}

// sanitize is the entry point for a raw line the client printed.
func (s *logSink) WriteLine(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.muted || strings.TrimSpace(raw) == "" {
		return
	}
	s.appendLocked(raw)
}

func (s *logSink) appendLocked(raw string) {
	level, when, text := parseLogLine(raw)
	text = compactMessage(text)
	// The client prints a ping and a bandwidth line every second; they are
	// heartbeats, and the dashboard already shows what they say. Keep them
	// behind the debug level so info stays readable.
	if level == "info" && isHeartbeat(text) {
		level = "debug"
	}
	if rank, ok := levelRank[level]; ok && rank < s.minLevel {
		return
	}
	s.seq++
	s.lines = append(s.lines, LogLine{Seq: s.seq, Time: when, Level: level, Text: text})
	if len(s.lines) > s.limit {
		s.lines = s.lines[len(s.lines)-s.limit:]
	}
}

func isHeartbeat(text string) bool {
	return strings.Contains(text, "Packet/s")
}

var (
	// The client logs a Go time.Time and its monotonic clock inside ping lines:
	// noise that wraps the line three times in a narrow panel.
	reStamp    = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(\.\d+)? \+\d{4} \S*`)
	reMono     = regexp.MustCompile(`\s*m=\+[\d.]+`)
	reSpaces   = regexp.MustCompile(`\s{2,}`)
)

// compactMessage keeps what a line says and drops clock readings that only
// repeat the timestamp the log already shows.
func compactMessage(text string) string {
	text = reStamp.ReplaceAllString(text, "")
	text = reMono.ReplaceAllString(text, "")
	return strings.TrimSpace(reSpaces.ReplaceAllString(text, " "))
}

// parseLogLine reads loggo's "[LEVEL] [time] [file:line] [func] message" and
// keeps the level, the clock time and the message.
func parseLogLine(raw string) (level, when, text string) {
	level, when, text = "info", time.Now().Format("15:04:05"), strings.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' {
		return level, when, text
	}
	if end := strings.IndexByte(raw, ']'); end > 1 {
		level = strings.ToLower(strings.TrimSpace(raw[1:end]))
		raw = strings.TrimLeft(raw[end+1:], " ")
	}
	if strings.HasPrefix(raw, "[") {
		if end := strings.IndexByte(raw, ']'); end > 1 {
			stamp := raw[1:end]
			if t, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
				when = t.Format("15:04:05")
			}
			raw = strings.TrimLeft(raw[end+1:], " ")
		}
	}
	// Drop the "[file:line] [func]" breadcrumbs: they say where in the client
	// something happened, which is noise for someone watching a tunnel.
	for i := 0; i < 2; i++ {
		if strings.HasPrefix(raw, "[") {
			if end := strings.IndexByte(raw, ']'); end > 1 && strings.Count(raw[:end], ":") <= 1 && !strings.Contains(raw[:end], " ") {
				raw = strings.TrimLeft(raw[end+1:], " ")
			}
		}
	}
	return level, when, strings.TrimSpace(raw)
}

// lastError returns the newest line that looks like an error, which is what a
// child process prints right before it exits.
func (s *logSink) lastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.lines) - 1; i >= 0; i-- {
		line := s.lines[i]
		if line.Level == "error" || strings.Contains(strings.ToLower(line.Text), "error listening") {
			return line.Text
		}
	}
	return ""
}

// snapshot returns every line, for a page that just loaded.
func (s *logSink) snapshot() []LogLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LogLine, len(s.lines))
	copy(out, s.lines)
	if len(out) > 400 {
		out = out[len(out)-400:]
	}
	return out
}

// since returns the lines a page has not seen yet.
func (s *logSink) since(seq int64) []LogLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lines) == 0 {
		return nil
	}
	i := len(s.lines)
	for i > 0 && s.lines[i-1].Seq > seq {
		i--
	}
	if i == len(s.lines) {
		return nil
	}
	out := make([]LogLine, len(s.lines)-i)
	copy(out, s.lines[i:])
	return out
}

func (s *logSink) lastSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}

func (s *logSink) debugf(format string, a ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.muted {
		return
	}
	s.seq++
	s.lines = append(s.lines, LogLine{
		Seq:   s.seq,
		Time:  time.Now().Format("15:04:05"),
		Level: "app",
		Text:  fmt.Sprintf(format, a...),
	})
}
