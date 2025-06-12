package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/guptarohit/asciigraph"
	probing "github.com/prometheus-community/pro-bing"
)

type Host struct {
	Address string
	Name    string
	Pinger  *probing.Pinger
	Stats   *Stats
	Logger  *log.Logger
	mu      sync.RWMutex
}

type Stats struct {
	Latencies   []float64 // Sliding window for sparkline graph
	PacketsSent int
	PacketsRecv int
	MinRTT      time.Duration
	MaxRTT      time.Duration
	AvgRTT      time.Duration
	StdDevRTT   time.Duration
	LastRTT     time.Duration
	// Lifetime statistics
	LifetimeSum       float64
	LifetimeCount     int
	LifetimeMinRTT    time.Duration
	LifetimeMaxRTT    time.Duration
	LifetimeSquareSum float64 // For standard deviation calculation
	// Lifetime distribution counts
	LifetimeDistribution [4]int // [<15ms, 15-50ms, 50-200ms, >200ms]
}

const (
	// Latency thresholds for sparkline color coding (in milliseconds)
	latencyGoodThreshold    = 50  // Below this is green
	latencyWarningThreshold = 150 // Below this is yellow, above is red

	// Latency thresholds for histogram categories (in milliseconds)
	latencyGreatThreshold     = 15  // Below this is "Great"
	latencyGoodUpperThreshold = 50  // Below this is "Good"
	latencyBadUpperThreshold  = 200 // Below this is "Bad", above is "Unusable"

	// Packet loss thresholds for border color (in percentage)
	packetLossWarningThreshold  = 2  // Above this is yellow
	packetLossCriticalThreshold = 10 // Above this is red
)

var (
	hosts = []struct {
		address string
		name    string
	}{
		{"192.168.1.1", "Gateway"},
		{"8.8.8.8", "Google DNS"},
		{"1.1.1.1", "Cloudflare DNS"},
	}
	maxDataPoints = 100
)

// Terminal UI using tcell
type TerminalUI struct {
	screen tcell.Screen
	width  int
	height int
}

func NewTerminalUI() (*TerminalUI, error) {
	screen, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}

	if err := screen.Init(); err != nil {
		return nil, err
	}

	w, h := screen.Size()
	return &TerminalUI{
		screen: screen,
		width:  w,
		height: h,
	}, nil
}

func (ui *TerminalUI) Close() {
	ui.screen.Fini()
}

func (ui *TerminalUI) Clear() {
	ui.screen.Clear()
}

func (ui *TerminalUI) DrawText(x, y int, text string, style tcell.Style) {
	col := x
	row := y
	for _, ch := range text {
		if ch == '\n' {
			row++
			col = x
			continue
		}
		if col < ui.width && row < ui.height {
			ui.screen.SetContent(col, row, ch, nil, style)
			col++
		}
	}
}

func (ui *TerminalUI) DrawBox(x, y, w, h int, title string, borderStyle tcell.Style) {
	// Draw corners
	ui.screen.SetContent(x, y, '┌', nil, borderStyle)
	ui.screen.SetContent(x+w-1, y, '┐', nil, borderStyle)
	ui.screen.SetContent(x, y+h-1, '└', nil, borderStyle)
	ui.screen.SetContent(x+w-1, y+h-1, '┘', nil, borderStyle)

	// Draw horizontal lines
	for i := 1; i < w-1; i++ {
		ui.screen.SetContent(x+i, y, '─', nil, borderStyle)
		ui.screen.SetContent(x+i, y+h-1, '─', nil, borderStyle)
	}

	// Draw vertical lines
	for i := 1; i < h-1; i++ {
		ui.screen.SetContent(x, y+i, '│', nil, borderStyle)
		ui.screen.SetContent(x+w-1, y+i, '│', nil, borderStyle)
	}

	// Draw title
	if title != "" {
		titleText := fmt.Sprintf(" %s ", title)
		titleStart := x + (w-len(titleText))/2
		ui.DrawText(titleStart, y, titleText, borderStyle.Bold(true))
	}
}

func (ui *TerminalUI) DrawGraph(x, y, width, height int, data []float64, title string, avgLatency float64, maxLatency float64) {
	const dx = 7
	// Determine border color based on average latency
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorGreen)
	if avgLatency >= latencyWarningThreshold {
		borderStyle = tcell.StyleDefault.Foreground(tcell.ColorRed)
	} else if avgLatency >= latencyGoodThreshold {
		borderStyle = tcell.StyleDefault.Foreground(tcell.ColorYellow)
	}

	// Draw box around graph
	ui.DrawBox(x, y, width, height, title, borderStyle)

	if len(data) == 0 {
		return
	}

	if width-dx < 0 {
		return
	}

	// Adjust data to fit width
	maxDataPoints := width - dx
	displayData := make([]float64, maxDataPoints)

	// Pad with leading zeros if we don't have enough data yet
	if len(data) < maxDataPoints {
		// Copy data to the end of displayData, leaving zeros at the beginning
		copy(displayData[maxDataPoints-len(data):], data)
	} else {
		// Take the most recent data points
		displayData = data[len(data)-maxDataPoints:]
	}

	// Generate graph using asciigraph
	graph := asciigraph.Plot(displayData,
		asciigraph.Height(height-2), // Account for box borders
		asciigraph.Width(width-dx),
		asciigraph.LowerBound(0), // Always start y-axis from 0
		asciigraph.UpperBound(maxLatency),
		asciigraph.Precision(1),
	)

	// Draw graph inside box
	lines := strings.Split(graph, "\n")
	graphStyle := tcell.StyleDefault.Foreground(tcell.ColorGreen)
	if avgLatency >= latencyWarningThreshold {
		graphStyle = tcell.StyleDefault.Foreground(tcell.ColorRed)
	} else if avgLatency >= latencyGoodThreshold {
		graphStyle = tcell.StyleDefault.Foreground(tcell.ColorYellow)
	}

	for i, line := range lines {
		if i < height-2 && y+1+i < ui.height {
			ui.DrawText(x+1, y+1+i, line, graphStyle)
		}
	}
}

func (ui *TerminalUI) DrawBarChart(x, y, width, height int, data []float64, labels []string, title string) {
	// Draw box
	ui.DrawBox(x, y, width, height, title, tcell.StyleDefault)

	if len(data) == 0 {
		return
	}

	// Calculate bar dimensions
	barArea := height - 4 // Account for box, labels, and spacing
	barWidth := (width-2)/len(data) - 1
	if barWidth < 3 {
		barWidth = 3
	}

	// Find max value for scaling
	maxVal := 0.0
	for _, v := range data {
		if v > maxVal {
			maxVal = v
		}
	}

	// Draw bars
	for i, val := range data {
		if i >= len(labels) {
			break
		}

		barX := x + 1 + i*(barWidth+1)
		if barX+barWidth > x+width-1 {
			break
		}

		// Calculate bar height
		barHeight := 0
		if maxVal > 0 {
			barHeight = int((val / maxVal) * float64(barArea))
		}

		// Choose color based on category
		barStyle := tcell.StyleDefault
		switch i {
		case 0:
			barStyle = barStyle.Foreground(tcell.ColorGreen)
		case 1:
			barStyle = barStyle.Foreground(tcell.ColorYellow)
		case 2:
			barStyle = barStyle.Foreground(tcell.ColorOlive)
		case 3:
			barStyle = barStyle.Foreground(tcell.ColorRed)
		}

		// Draw bar
		for h := 0; h < barHeight; h++ {
			barY := y + height - 3 - h
			for w := 0; w < barWidth; w++ {
				if barX+w < x+width-1 {
					ui.screen.SetContent(barX+w, barY, '█', nil, barStyle)
				}
			}
		}

		// Draw percentage
		pctText := fmt.Sprintf("%.1f%%", val)
		pctX := barX + (barWidth-len(pctText))/2
		if pctX > x && pctX+len(pctText) < x+width {
			ui.DrawText(pctX, y+height-3-barHeight-1, pctText, tcell.StyleDefault)
		}

		// Draw label
		labelX := barX + (barWidth-len(labels[i]))/2
		if labelX > x && labelX+len(labels[i]) < x+width {
			ui.DrawText(labelX, y+height-2, labels[i], tcell.StyleDefault.Dim(true))
		}
	}
}

func (ui *TerminalUI) DrawStats(x, y, width, height int, host *Host) {
	host.mu.RLock()
	defer host.mu.RUnlock()

	// Calculate packet loss
	packetLoss := 0.0
	packetsLost := 0
	if host.Stats.PacketsSent > 0 {
		packetsLost = host.Stats.PacketsSent - host.Stats.PacketsRecv
		packetLoss = float64(packetsLost) / float64(host.Stats.PacketsSent) * 100
	}

	// Determine border color based on packet loss
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorGreen)
	if packetsLost > packetLossCriticalThreshold {
		borderStyle = tcell.StyleDefault.Foreground(tcell.ColorRed)
	} else if packetsLost > packetLossWarningThreshold {
		borderStyle = tcell.StyleDefault.Foreground(tcell.ColorYellow)
	}

	ui.DrawBox(x, y, width, height, "Statistics", borderStyle)

	// Calculate lifetime statistics
	var lifetimeAvg, lifetimeStdDev time.Duration
	if host.Stats.LifetimeCount > 0 {
		avgMs := host.Stats.LifetimeSum / float64(host.Stats.LifetimeCount)
		lifetimeAvg = time.Duration(avgMs) * time.Millisecond

		meanSquare := host.Stats.LifetimeSquareSum / float64(host.Stats.LifetimeCount)
		variance := meanSquare - (avgMs * avgMs)
		if variance > 0 {
			lifetimeStdDev = time.Duration(math.Sqrt(variance)) * time.Millisecond
		}
	}

	// Draw stats
	row := y + 1
	ui.DrawText(x+1, row, fmt.Sprintf("Packets: %d/%d", host.Stats.PacketsRecv, host.Stats.PacketsSent), tcell.StyleDefault)
	row++
	ui.DrawText(x+1, row, fmt.Sprintf("Loss: %.1f%% (%d)", packetLoss, packetsLost), tcell.StyleDefault)
	row++
	ui.DrawText(x+1, row, fmt.Sprintf("Last: %v", host.Stats.LastRTT.Round(time.Millisecond)), tcell.StyleDefault)
	row++
	ui.DrawText(x+1, row, fmt.Sprintf("Min: %v", func() time.Duration {
		if host.Stats.LifetimeMinRTT == time.Duration(math.MaxInt64) {
			return 0
		}
		return host.Stats.LifetimeMinRTT.Round(time.Millisecond)
	}()), tcell.StyleDefault)
	row++
	ui.DrawText(x+1, row, fmt.Sprintf("Avg: %v", lifetimeAvg.Round(time.Millisecond)), tcell.StyleDefault)
	row++
	ui.DrawText(x+1, row, fmt.Sprintf("Max: %v", host.Stats.LifetimeMaxRTT.Round(time.Millisecond)), tcell.StyleDefault)
	row++
	ui.DrawText(x+1, row, fmt.Sprintf("StdDev: %v", lifetimeStdDev.Round(time.Millisecond)), tcell.StyleDefault)

	// Draw distribution if we have data
	if host.Stats.LifetimeCount > 0 && row+6 < y+height-1 {
		row += 2
		ui.DrawText(x+1, row, "Distribution:", tcell.StyleDefault.Bold(true))
		row++
		ui.DrawText(x+1, row, fmt.Sprintf("<%dms: %d", latencyGreatThreshold, host.Stats.LifetimeDistribution[0]), tcell.StyleDefault)
		row++
		ui.DrawText(x+1, row, fmt.Sprintf("%d-%dms: %d", latencyGreatThreshold, latencyGoodUpperThreshold, host.Stats.LifetimeDistribution[1]), tcell.StyleDefault)
		row++
		ui.DrawText(x+1, row, fmt.Sprintf("%d-%dms: %d", latencyGoodUpperThreshold, latencyBadUpperThreshold, host.Stats.LifetimeDistribution[2]), tcell.StyleDefault)
		row++
		ui.DrawText(x+1, row, fmt.Sprintf(">%dms: %d", latencyBadUpperThreshold, host.Stats.LifetimeDistribution[3]), tcell.StyleDefault)
	}
}

func (ui *TerminalUI) Show() {
	ui.screen.Show()
}

func main() {
	// Parse command line flags
	routerIP := flag.String("router", "192.168.1.1", "Router/Gateway IP address")
	flag.Parse()

	// Update the gateway address with the provided router IP
	hosts[0].address = *routerIP

	// Create logs directory
	if err := os.MkdirAll("logs", 0755); err != nil {
		log.Fatalf("failed to create logs directory: %v", err)
	}

	// Create timestamp for this run
	runTimestamp := time.Now().Format("20060102_150405")

	// Set up main log file with timestamp
	mainLogFile, err := os.OpenFile(fmt.Sprintf("logs/pwnping_%s.log", runTimestamp), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("failed to open main log file: %v", err)
	}
	defer mainLogFile.Close()
	log.SetOutput(mainLogFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	log.Println("Starting pwnping...")

	// Initialize terminal UI
	ui, err := NewTerminalUI()
	if err != nil {
		log.Fatalf("failed to initialize terminal UI: %v", err)
	}
	defer ui.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hostMonitors := make([]*Host, 0)

	for _, h := range hosts {
		host := &Host{
			Address: h.address,
			Name:    h.name,
			Stats: &Stats{
				LifetimeMinRTT: time.Duration(math.MaxInt64),
			},
		}

		// Create per-host log file with timestamp
		hostLogFile, err := os.OpenFile(fmt.Sprintf("logs/%s_%s.log", h.address, runTimestamp), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("failed to create log file for %s: %v", h.address, err)
			continue
		}
		defer hostLogFile.Close()

		// Create logger for this host
		host.Logger = log.New(hostLogFile, fmt.Sprintf("[%s] ", h.address), log.Ldate|log.Ltime|log.Lmicroseconds)
		host.Logger.Printf("Starting monitor for %s (%s)", h.name, h.address)

		pinger, err := probing.NewPinger(h.address)
		if err != nil {
			log.Printf("failed to create pinger for %s: %v", h.address, err)
			host.Logger.Printf("Failed to create pinger: %v", err)
			continue
		}
		pinger.SetPrivileged(true)
		pinger.Interval = time.Second
		pinger.Count = -1         // -1 means ping forever
		pinger.RecordRtts = false // Don't record RTTs in pinger's internal slice
		pinger.RecordTTLs = false // Don't record TTLs in pinger's internal slice
		host.Logger.Printf("Created pinger with Count=%d, Interval=%v", pinger.Count, pinger.Interval)
		host.Pinger = pinger

		hostMonitors = append(hostMonitors, host)

		go monitorHost(ctx, host)
	}

	if len(hostMonitors) == 0 {
		log.Fatal("No hosts could be monitored. Please check your network configuration.")
	}

	// Event handling
	eventCh := make(chan tcell.Event)
	go func() {
		for {
			eventCh <- ui.screen.PollEvent()
		}
	}()

	// Update ticker
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	// Main loop
	for {
		select {
		case ev := <-eventCh:
			switch ev := ev.(type) {
			case *tcell.EventKey:
				if ev.Key() == tcell.KeyEscape || ev.Rune() == 'q' || ev.Key() == tcell.KeyCtrlC {
					return
				}
			case *tcell.EventResize:
				ui.width, ui.height = ui.screen.Size()
				ui.screen.Sync()
			}
		case <-ticker.C:
			ui.Clear()
			drawUI(ui, hostMonitors)
			ui.Show()
		case <-ctx.Done():
			return
		}
	}
}

func drawUI(ui *TerminalUI, hostMonitors []*Host) {
	// Draw title
	startTime := time.Now().Format("2006-01-02 15:04:05")
	titleLines := []string{
		" ____                 ____  _             ",
		"|  _ \\__      ___ __ |  _ \\(_)_ __   __ _ ",
		"| |_) \\ \\ /\\ / / '_ \\| |_) | | '_ \\ / _` |           Pinging local gateway, Google DNS, and Cloudflare DNS.",
		"|  __/ \\ V  V /| | | |  __/| | | | | (_| |           Started at: " + startTime,
		"|_|     \\_/\\_/ |_| |_|_|   |_|_| |_|\\__, |",
		"                                    |___/            Press 'q' to quit.",
	}

	titleStyle := tcell.StyleDefault.Foreground(tcell.ColorTeal)
	for i, line := range titleLines {
		ui.DrawText(0, i, line, titleStyle)
	}

	// Calculate layout
	titleHeight := 7
	availableHeight := ui.height - titleHeight
	hostHeight := availableHeight / len(hostMonitors)
	if hostHeight < 10 {
		hostHeight = 10
	}

	// Draw each host
	for i, host := range hostMonitors {
		y := titleHeight + i*hostHeight

		// Calculate widths (55% graph, 25% histogram, 20% stats)
		graphWidth := int(float64(ui.width) * 0.55)
		histWidth := int(float64(ui.width) * 0.25)

		// Draw title for this host
		hostTitle := fmt.Sprintf("%s (%s)", host.Name, host.Address)
		ui.DrawText(2, y, hostTitle, tcell.StyleDefault.Bold(true))

		// Draw graph
		host.mu.RLock()
		data := make([]float64, len(host.Stats.Latencies))
		copy(data, host.Stats.Latencies)
		avgLatency := float64(host.Stats.AvgRTT.Milliseconds())

		// Get max latency for graph title
		maxLatency := 0.0
		for _, lat := range data {
			if lat > maxLatency {
				maxLatency = lat
			}
		}
		graphTitle := fmt.Sprintf("Latency - Max: %.0fms", maxLatency)
		host.mu.RUnlock()

		// Calculate widget positions without spacing
		graphActualWidth := graphWidth
		histX := graphActualWidth
		histActualWidth := histWidth
		statsX := histX + histActualWidth
		statsActualWidth := ui.width - statsX

		ui.DrawGraph(0, y+1, graphActualWidth, hostHeight-2, data, graphTitle, avgLatency, maxLatency)

		// Draw histogram
		host.mu.RLock()
		histData := make([]float64, 4)
		labels := []string{
			fmt.Sprintf("<%dms", latencyGreatThreshold),
			fmt.Sprintf("%d-%d", latencyGreatThreshold, latencyGoodUpperThreshold),
			fmt.Sprintf("%d-%d", latencyGoodUpperThreshold, latencyBadUpperThreshold),
			fmt.Sprintf(">%dms", latencyBadUpperThreshold),
		}

		if host.Stats.LifetimeCount > 0 {
			total := float64(host.Stats.LifetimeCount)
			for i := 0; i < 4; i++ {
				histData[i] = math.Round((float64(host.Stats.LifetimeDistribution[i])/total)*1000) / 10
			}
		}
		host.mu.RUnlock()

		ui.DrawBarChart(histX, y+1, histActualWidth, hostHeight-2, histData, labels, "Distribution (%)")

		// Draw stats
		ui.DrawStats(statsX, y+1, statsActualWidth, hostHeight-2, host)
	}
}

func monitorHost(ctx context.Context, host *Host) {
	host.Logger.Printf("Starting monitor")

	host.Pinger.OnRecv = func(pkt *probing.Packet) {
		host.mu.Lock()
		defer host.mu.Unlock()

		rtt := pkt.Rtt

		// Validate RTT - check for negative values
		if rtt < 0 {
			host.Logger.Printf("WARNING: Negative RTT detected! Packet seq=%d, RTT=%v", pkt.Seq, rtt)
			// Skip this packet to avoid corrupting statistics
			return
		}

		// Also check for unreasonably high RTT values (> 30 seconds)
		if rtt > 30*time.Second {
			host.Logger.Printf("WARNING: Unreasonably high RTT detected! Packet seq=%d, RTT=%v", pkt.Seq, rtt)
			// Skip this packet
			return
		}

		host.Stats.LastRTT = rtt
		rttMs := float64(rtt.Milliseconds())

		// Update sliding window for graph
		host.Stats.Latencies = append(host.Stats.Latencies, rttMs)
		if len(host.Stats.Latencies) > maxDataPoints {
			host.Stats.Latencies = host.Stats.Latencies[1:]
		}

		// Update lifetime statistics
		host.Stats.LifetimeSum += rttMs
		host.Stats.LifetimeSquareSum += rttMs * rttMs
		host.Stats.LifetimeCount++

		if rtt < host.Stats.LifetimeMinRTT {
			host.Stats.LifetimeMinRTT = rtt
		}
		if rtt > host.Stats.LifetimeMaxRTT {
			host.Stats.LifetimeMaxRTT = rtt
		}

		// Update lifetime distribution
		switch {
		case rttMs < float64(latencyGreatThreshold):
			host.Stats.LifetimeDistribution[0]++
		case rttMs < float64(latencyGoodUpperThreshold):
			host.Stats.LifetimeDistribution[1]++
		case rttMs < float64(latencyBadUpperThreshold):
			host.Stats.LifetimeDistribution[2]++
		default:
			host.Stats.LifetimeDistribution[3]++
		}

		host.Stats.PacketsRecv++
		updateStats(host.Stats)
		host.Logger.Printf("Received packet seq=%d, RTT=%v", pkt.Seq, rtt)
	}

	host.Pinger.OnSend = func(pkt *probing.Packet) {
		host.mu.Lock()
		host.Stats.PacketsSent++
		sent := host.Stats.PacketsSent
		host.mu.Unlock()
		host.Logger.Printf("Sent packet seq=%d (total sent: %d)", pkt.Seq, sent)
	}

	host.Pinger.OnDuplicateRecv = func(pkt *probing.Packet) {
		host.Logger.Printf("Duplicate packet received: seq=%d, RTT=%v", pkt.Seq, pkt.Rtt)
	}

	host.Pinger.OnFinish = func(stats *probing.Statistics) {
		// This shouldn't be called if Count=-1, but let's check
		host.Logger.Printf("WARNING: Pinger finished unexpectedly. Packets sent: %d, received: %d",
			stats.PacketsSent, stats.PacketsRecv)
	}

	host.Logger.Printf("Running pinger")
	err := host.Pinger.RunWithContext(ctx)
	host.Logger.Printf("Pinger stopped with error: %v", err)
}

func updateStats(stats *Stats) {
	// Update sliding window stats for graph color
	if len(stats.Latencies) > 0 {
		stats.MinRTT = time.Duration(math.MaxInt64)
		stats.MaxRTT = 0
		sum := 0.0

		for _, lat := range stats.Latencies {
			rtt := time.Duration(lat) * time.Millisecond
			if rtt < stats.MinRTT {
				stats.MinRTT = rtt
			}
			if rtt > stats.MaxRTT {
				stats.MaxRTT = rtt
			}
			sum += lat
		}

		avg := sum / float64(len(stats.Latencies))
		stats.AvgRTT = time.Duration(avg) * time.Millisecond

		variance := 0.0
		for _, lat := range stats.Latencies {
			variance += math.Pow(lat-avg, 2)
		}
		stdDev := math.Sqrt(variance / float64(len(stats.Latencies)))
		stats.StdDevRTT = time.Duration(stdDev) * time.Millisecond
	}
}
