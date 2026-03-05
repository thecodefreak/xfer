package client

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Progress struct {
	mu          sync.Mutex
	enabled     bool
	fileName    string
	fileIndex   int
	totalFiles  int
	bytesSent   int64
	totalBytes  int64
	startTime   time.Time
	lastUpdate  time.Time
	lastBytes   int64
	bytesPerSec float64
	completed   bool
	lastWidth   int
}

func NewProgress(enabled bool) *Progress {
	return &Progress{
		enabled:    enabled,
		startTime:  time.Now(),
		lastUpdate: time.Now(),
	}
}

func (p *Progress) SetFile(name string, index, total int, size int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.fileName = name
	p.fileIndex = index
	p.totalFiles = total
	p.totalBytes = size
	p.bytesSent = 0
	p.startTime = time.Now()
	p.lastUpdate = time.Now()
	p.lastBytes = 0
	p.completed = false
	p.lastWidth = 0
}

func (p *Progress) Update(bytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.bytesSent += bytes

	now := time.Now()
	elapsed := now.Sub(p.lastUpdate).Seconds()
	if elapsed >= 0.5 {
		bytesDiff := p.bytesSent - p.lastBytes
		p.bytesPerSec = float64(bytesDiff) / elapsed
		p.lastUpdate = now
		p.lastBytes = p.bytesSent
	}
}

func (p *Progress) SetTotal(bytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.totalBytes = bytes
}

func (p *Progress) Complete() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.completed = true
	p.bytesSent = p.totalBytes
}

func (p *Progress) Render() string {
	if !p.enabled {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.completed {
		line := p.padLine(p.formatComplete())
		return fmt.Sprintf("\r%s\n", line)
	}

	line := p.padLine(p.formatProgress())
	return fmt.Sprintf("\r%s", line)
}

func (p *Progress) formatProgress() string {
	fileInfo := p.fileName
	if p.totalFiles > 1 {
		fileInfo = fmt.Sprintf("[%d/%d] %s", p.fileIndex+1, p.totalFiles, p.fileName)
	}

	percent := 0.0
	if p.totalBytes > 0 {
		percent = float64(p.bytesSent) / float64(p.totalBytes) * 100
	}

	barWidth := 30
	filled := int(percent / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	speed := formatSpeed(p.bytesPerSec)

	eta := "calculating..."
	if p.bytesPerSec > 0 && p.totalBytes > 0 {
		remaining := p.totalBytes - p.bytesSent
		seconds := float64(remaining) / p.bytesPerSec
		eta = formatDuration(time.Duration(seconds) * time.Second)
	}

	sizeProgress := fmt.Sprintf("%s / %s", formatSize(p.bytesSent), formatSize(p.totalBytes))

	return fmt.Sprintf("%s [%s] %5.1f%%  %s  ETA %s  %s",
		fileInfo, bar, percent, speed, eta, sizeProgress)
}

func (p *Progress) formatComplete() string {
	elapsed := time.Since(p.startTime)
	avgSpeed := 0.0
	if elapsed.Seconds() > 0 {
		avgSpeed = float64(p.totalBytes) / elapsed.Seconds()
	}

	return fmt.Sprintf("✓ %s (%s) transferred in %s (%s)",
		p.fileName,
		formatSize(p.totalBytes),
		formatDuration(elapsed),
		formatSpeed(avgSpeed))
}

func (p *Progress) padLine(line string) string {
	width := utf8.RuneCountInString(line)
	if width < p.lastWidth {
		line += strings.Repeat(" ", p.lastWidth-width)
	}
	p.lastWidth = width
	return line
}

func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
}

func formatSpeed(bytesPerSec float64) string {
	if bytesPerSec < 1024 {
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
	if bytesPerSec < 1024*1024 {
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/1024)
	}
	if bytesPerSec < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB/s", bytesPerSec/(1024*1024*1024))
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

type Spinner struct {
	frames  []string
	current int
	message string
	mu      sync.Mutex
}

func NewSpinner(message string) *Spinner {
	return &Spinner{
		frames:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		message: message,
	}
}

func (s *Spinner) Tick() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	frame := s.frames[s.current]
	s.current = (s.current + 1) % len(s.frames)
	return fmt.Sprintf("\r%s %s", frame, s.message)
}

func (s *Spinner) SetMessage(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = msg
}

func (s *Spinner) Clear() string {
	return "\r" + strings.Repeat(" ", 60) + "\r"
}
