package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sync"
	"time"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
	probing "github.com/prometheus-community/pro-bing"
)

type Host struct {
	Address     string
	Name        string
	Pinger      *probing.Pinger
	Stats       *Stats
	Graph       *widgets.Sparkline
	GraphGroup  *widgets.SparklineGroup
	Histogram   *widgets.BarChart
	StatsWidget *widgets.Paragraph
	Logger      *log.Logger
	mu          sync.RWMutex
}

type Stats struct {
	Latencies   []float64
	PacketsSent int
	PacketsRecv int
	MinRTT      time.Duration
	MaxRTT      time.Duration
	AvgRTT      time.Duration
	StdDevRTT   time.Duration
	LastRTT     time.Duration
}

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
	histogramBins = 10
)

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

	if err := ui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer ui.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hostMonitors := make([]*Host, 0)
	grid := ui.NewGrid()

	for _, h := range hosts {
		host := &Host{
			Address: h.address,
			Name:    h.name,
			Stats:   &Stats{},
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

		host.Graph = widgets.NewSparkline()
		host.Graph.Title = fmt.Sprintf("%s Latency", h.name)
		host.Graph.LineColor = ui.ColorGreen

		host.GraphGroup = widgets.NewSparklineGroup(host.Graph)
		host.GraphGroup.Title = fmt.Sprintf("%s (%s)", h.name, h.address)

		host.Histogram = widgets.NewBarChart()
		host.Histogram.Title = "Latency Distribution (%)"
		host.Histogram.BarWidth = 8
		host.Histogram.LabelStyles = []ui.Style{ui.NewStyle(ui.ColorWhite)}
		host.Histogram.NumStyles = []ui.Style{ui.NewStyle(ui.ColorWhite)}
		host.Histogram.BarColors = []ui.Color{ui.ColorGreen, ui.ColorYellow, ui.ColorMagenta, ui.ColorRed}

		host.StatsWidget = widgets.NewParagraph()
		host.StatsWidget.Title = "Statistics"
		host.StatsWidget.TextStyle = ui.NewStyle(ui.ColorWhite)

		hostMonitors = append(hostMonitors, host)

		go monitorHost(ctx, host)
	}

	if len(hostMonitors) == 0 {
		log.Fatal("No hosts could be monitored. Please check your network configuration.")
	}

	termWidth, termHeight := ui.TerminalDimensions()
	grid.SetRect(0, 0, termWidth, termHeight)

	// Create title widget
	title := widgets.NewParagraph()
	startTime := time.Now().Format("2006-01-02 15:04:05")
	title.Text = fmt.Sprintf(
		" ____                 ____  _             \n"+
			"|  _ \\__      ___ __ |  _ \\(_)_ __   __ _ \n"+
			"| |_) \\ \\ /\\ / / '_ \\| |_) | | '_ \\ / _` |           Pinging local gateway, Google DNS, and Cloudflare DNS.\n"+
			"|  __/ \\ V  V /| | | |  __/| | | | | (_| |           Started at: %s\n"+
			"|_|     \\_/\\_/ |_| |_|_|   |_|_| |_|\\__, |\n"+
			"                                    |___/            Press 'q' to quit.", startTime)
	title.TextStyle.Fg = ui.ColorCyan
	title.Border = false

	rows := make([]interface{}, 0)

	// Add title as first row
	rows = append(rows, ui.NewRow(0.15, ui.NewCol(1.0, title)))

	// Calculate remaining height for host rows
	remainingHeight := 0.85
	hostRowHeight := remainingHeight / float64(len(hostMonitors))

	for _, host := range hostMonitors {
		row := ui.NewRow(hostRowHeight,
			ui.NewCol(0.55, host.GraphGroup),
			ui.NewCol(0.25, host.Histogram),
			ui.NewCol(0.20, host.StatsWidget),
		)
		rows = append(rows, row)
	}

	grid.Set(rows...)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	uiEvents := ui.PollEvents()
	for {
		select {
		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				return
			case "<Resize>":
				payload := e.Payload.(ui.Resize)
				grid.SetRect(0, 0, payload.Width, payload.Height)
				// Update all host UIs to adapt to new terminal size
				for _, host := range hostMonitors {
					updateUI(host)
				}
				ui.Clear()
				ui.Render(grid)
			}
		case <-ticker.C:
			for _, host := range hostMonitors {
				updateUI(host)
			}
			ui.Render(grid)
		case <-ctx.Done():
			return
		}
	}
}

func monitorHost(ctx context.Context, host *Host) {
	host.Logger.Printf("Starting monitor")

	host.Pinger.OnRecv = func(pkt *probing.Packet) {
		host.mu.Lock()
		defer host.mu.Unlock()

		rtt := pkt.Rtt
		host.Stats.LastRTT = rtt
		host.Stats.Latencies = append(host.Stats.Latencies, float64(rtt.Milliseconds()))

		if len(host.Stats.Latencies) > maxDataPoints {
			host.Stats.Latencies = host.Stats.Latencies[1:]
		}

		host.Stats.PacketsRecv++
		updateStats(host.Stats)
		host.Logger.Printf("Received packet %d, RTT=%v", host.Stats.PacketsRecv, rtt)
	}

	host.Pinger.OnSend = func(pkt *probing.Packet) {
		host.mu.Lock()
		host.Stats.PacketsSent++
		sent := host.Stats.PacketsSent
		host.mu.Unlock()
		host.Logger.Printf("Sent packet %d", sent)
	}

	host.Pinger.OnDuplicateRecv = func(pkt *probing.Packet) {
		host.Logger.Printf("Duplicate packet received")
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
	if len(stats.Latencies) == 0 {
		return
	}

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

func updateUI(host *Host) {
	host.mu.RLock()
	defer host.mu.RUnlock()

	if len(host.Stats.Latencies) > 0 {
		// Calculate the approximate width available for the sparkline
		// The sparkline takes 55% of terminal width (our new column ratio), minus borders and padding
		termWidth, _ := ui.TerminalDimensions()
		sparklineWidth := int(float64(termWidth)*0.55) - 2 // Account for borders and padding

		// Only display as many data points as can fit in the available width
		// Each data point takes approximately 1 character width
		dataToDisplay := host.Stats.Latencies
		if len(dataToDisplay) > sparklineWidth && sparklineWidth > 0 {
			// Show the most recent data points that fit
			startIdx := len(dataToDisplay) - sparklineWidth
			dataToDisplay = dataToDisplay[startIdx:]
		}

		host.Graph.Data = dataToDisplay

		// Update y-axis labels based on current data
		maxLatency := 0.0
		for _, lat := range host.Stats.Latencies {
			if lat > maxLatency {
				maxLatency = lat
			}
		}

		// Set adaptive y-axis labels: 0, middle, max
		if maxLatency > 0 {
			host.Graph.MaxVal = maxLatency
			host.GraphGroup.Title = fmt.Sprintf("%s (%s) - Max: %.0fms", host.Name, host.Address, maxLatency)
		}

		avgLatency := float64(host.Stats.AvgRTT.Milliseconds())
		if avgLatency < 50 {
			host.Graph.LineColor = ui.ColorGreen
		} else if avgLatency < 150 {
			host.Graph.LineColor = ui.ColorYellow
		} else {
			host.Graph.LineColor = ui.ColorRed
		}

		updateHistogram(host)
	}

	packetLoss := 0.0
	packetsLost := 0
	if host.Stats.PacketsSent > 0 {
		packetsLost = host.Stats.PacketsSent - host.Stats.PacketsRecv
		packetLoss = float64(packetsLost) / float64(host.Stats.PacketsSent) * 100
	}

	statsText := fmt.Sprintf(
		"Packets: %d/%d\n"+
			"Loss: %.1f%% (%d)\n"+
			"Last: %v\n"+
			"Min: %v\n"+
			"Avg: %v\n"+
			"Max: %v\n"+
			"StdDev: %v",
		host.Stats.PacketsRecv, host.Stats.PacketsSent,
		packetLoss, packetsLost,
		host.Stats.LastRTT.Round(time.Millisecond),
		host.Stats.MinRTT.Round(time.Millisecond),
		host.Stats.AvgRTT.Round(time.Millisecond),
		host.Stats.MaxRTT.Round(time.Millisecond),
		host.Stats.StdDevRTT.Round(time.Millisecond),
	)
	host.StatsWidget.Text = statsText

	if packetsLost > 10 {
		host.StatsWidget.BorderStyle = ui.NewStyle(ui.ColorRed)
	} else if packetsLost > 2 {
		host.StatsWidget.BorderStyle = ui.NewStyle(ui.ColorYellow)
	} else {
		host.StatsWidget.BorderStyle = ui.NewStyle(ui.ColorGreen)
	}
}

func updateHistogram(host *Host) {
	if len(host.Stats.Latencies) == 0 {
		return
	}

	// Latency categories: Great (<15ms), Good (15-50ms), Bad (50-200ms), Unusable (200ms+)
	counts := make([]float64, 4)
	labels := []string{"<15ms", "15-50", "50-200", ">200ms"}
	barColors := []ui.Color{ui.ColorGreen, ui.ColorYellow, ui.ColorMagenta, ui.ColorRed}

	total := float64(len(host.Stats.Latencies))

	for _, lat := range host.Stats.Latencies {
		switch {
		case lat < 15:
			counts[0]++
		case lat < 50:
			counts[1]++
		case lat < 200:
			counts[2]++
		default:
			counts[3]++
		}
	}

	// Convert to percentages and round to 1 decimal place
	percentages := make([]float64, 4)
	for i, count := range counts {
		percentages[i] = math.Round((count/total)*1000) / 10 // Round to 1 decimal
	}

	host.Histogram.Data = percentages
	host.Histogram.Labels = labels
	host.Histogram.BarColors = barColors
}
