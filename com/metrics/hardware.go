// com/hardware/native.go
package metrics

import (
	"context"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type Snapshot struct {
	CollectedAt int64        `json:"collectedAt"`
	CPU         CPUMetrics   `json:"cpu"`
	Memory      MemMetrics   `json:"memory"`
	Disks       []DiskStats  `json:"disks"`
	Network     NetMetrics   `json:"network"`
	GPU         []GPUMetrics `json:"gpu,omitempty"`
	SystemPower *float64     `json:"systemPowerW,omitempty"`
}

type CPUMetrics struct {
	UtilizationPct *float64  `json:"utilizationPct,omitempty"` // overall
	PerCorePct     []float64 `json:"perCorePct,omitempty"`
	ClockMHz       *float64  `json:"clockMHz,omitempty"`      // base if present
	PackagePowerW  *float64  `json:"packagePowerW,omitempty"` // enhanced
	TemperatureC   *float64  `json:"temperatureC,omitempty"`  // enhanced
	LogicalCores   *int      `json:"logicalCores,omitempty"`
	PhysicalCores  *int      `json:"physicalCores,omitempty"`
}

type MemMetrics struct {
	Total   uint64  `json:"totalBytes"`
	Used    uint64  `json:"usedBytes"`
	Free    uint64  `json:"freeBytes"`
	UsedPct float64 `json:"usedPct"`
}

type DiskStats struct {
	Mountpoint     string   `json:"mountpoint"`
	Device         string   `json:"device,omitempty"`
	Total          *uint64  `json:"totalBytes,omitempty"`
	Used           *uint64  `json:"usedBytes,omitempty"`
	Free           *uint64  `json:"freeBytes,omitempty"`
	UsedPct        *float64 `json:"usedPct,omitempty"`
	TemperatureC   *float64 `json:"temperatureC,omitempty"` // enhanced
	ReadPerSec     *float64 `json:"readBytesPerSec,omitempty"`
	WritePerSec    *float64 `json:"writeBytesPerSec,omitempty"`
	IOReadCount    *uint64  `json:"ioReadCount,omitempty"`
	IOWriteCount   *uint64  `json:"ioWriteCount,omitempty"`
	SMARTHealth    *string  `json:"smartHealth,omitempty"`
	IsLiveOutputFS bool     `json:"isLiveOutputFS"`
}

type NetMetrics struct {
	BytesSent   uint64   `json:"bytesSent"`
	BytesRecv   uint64   `json:"bytesRecv"`
	PacketsSent uint64   `json:"packetsSent"`
	PacketsRecv uint64   `json:"packetsRecv"`
	Interfaces  []string `json:"interfaces,omitempty"`
}

type GPUMetrics struct {
	Vendor         string   `json:"vendor"`
	Name           string   `json:"name"`
	UtilizationPct *float64 `json:"utilizationPct,omitempty"`
	ClockMHz       *float64 `json:"clockMHz,omitempty"`
	PowerW         *float64 `json:"powerW,omitempty"`
	TemperatureC   *float64 `json:"temperatureC,omitempty"`
	MemoryTotalB   *uint64  `json:"memoryTotalBytes,omitempty"`
	MemoryUsedB    *uint64  `json:"memoryUsedBytes,omitempty"`
}

// CollectNative returns a snapshot using OS-native sources (gopsutil) plus enhanced metrics.
func CollectNative(ctx context.Context, liveOutputPath string) (Snapshot, error) {
	ts := time.Now().Unix()

	// CPU load (brief sample)
	var cpuSnap CPUMetrics
	if pct, err := cpu.PercentWithContext(ctx, 250*time.Millisecond, false); err == nil && len(pct) > 0 {
		v := pct[0]
		cpuSnap.UtilizationPct = &v
	}
	if per, err := cpu.PercentWithContext(ctx, 250*time.Millisecond, true); err == nil && len(per) > 0 {
		cpuSnap.PerCorePct = per
	}
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		// base clock (may be missing/0 on some systems)
		if infos[0].Mhz > 0 {
			m := infos[0].Mhz
			cpuSnap.ClockMHz = &m
		}
	}
	if phys, err := cpu.CountsWithContext(ctx, false); err == nil {
		cpuSnap.PhysicalCores = &phys
	}
	if logi, err := cpu.CountsWithContext(ctx, true); err == nil {
		cpuSnap.LogicalCores = &logi
	}

	// Enhanced CPU temperature collection
	cpuSnap.TemperatureC = getCPUTemperature(ctx)

	// Enhanced CPU package power collection
	cpuSnap.PackagePowerW = getCPUPackagePower(ctx)

	// Memory
	memSnap := MemMetrics{}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		memSnap.Total = vm.Total
		memSnap.Free = vm.Available
		memSnap.Used = vm.Total - vm.Available
		memSnap.UsedPct = vm.UsedPercent
	}

	// Enhanced disks with temperature
	var disksOut []DiskStats
	parts, _ := disk.PartitionsWithContext(ctx, true)
	ioMap, _ := disk.IOCountersWithContext(ctx)
	uptime, _ := host.UptimeWithContext(ctx)
	liveMount := mountOfPath(liveOutputPath, parts)

	for _, p := range parts {
		stat := DiskStats{
			Mountpoint:     p.Mountpoint,
			Device:         p.Device,
			IsLiveOutputFS: (liveMount != "" && equalFold(liveMount, p.Mountpoint)),
		}
		if u, err := disk.UsageWithContext(ctx, p.Mountpoint); err == nil {
			t, uB, f := u.Total, u.Used, u.Free
			up := u.UsedPercent
			stat.Total = &t
			stat.Used = &uB
			stat.Free = &f
			stat.UsedPct = &up
		}

		// Enhanced: Get disk temperature
		stat.TemperatureC = getDiskTemperature(p.Device)

		// Map IO counters (best-effort match)
		for dev, io := range ioMap {
			if dev == p.Device || matchDeviceLoose(dev, p.Device) {
				rc, wc := io.ReadCount, io.WriteCount
				stat.IOReadCount = &rc
				stat.IOWriteCount = &wc
				if uptime > 0 {
					rps := float64(io.ReadBytes) / float64(uptime)
					wps := float64(io.WriteBytes) / float64(uptime)
					stat.ReadPerSec = &rps
					stat.WritePerSec = &wps
				}
				break
			}
		}
		disksOut = append(disksOut, stat)
	}

	// Network (cumulative since boot)
	var netSnap NetMetrics
	if ns, err := net.IOCountersWithContext(ctx, false); err == nil && len(ns) > 0 {
		netSnap.BytesSent = ns[0].BytesSent
		netSnap.BytesRecv = ns[0].BytesRecv
		netSnap.PacketsSent = ns[0].PacketsSent
		netSnap.PacketsRecv = ns[0].PacketsRecv
	}
	if ifs, err := net.InterfacesWithContext(ctx); err == nil {
		for _, i := range ifs {
			netSnap.Interfaces = append(netSnap.Interfaces, i.Name)
		}
	}

	// Enhanced: GPU metrics collection
	gpuMetrics := collectGPUMetrics(ctx)

	// Enhanced: System power collection
	systemPower := getSystemPower(ctx)

	return Snapshot{
		CollectedAt: ts,
		CPU:         cpuSnap,
		Memory:      memSnap,
		Disks:       disksOut,
		Network:     netSnap,
		GPU:         gpuMetrics,
		SystemPower: systemPower,
	}, nil
}

// Enhanced CPU Temperature Collection
func getCPUTemperature(ctx context.Context) *float64 {
	// Try gopsutil first (works on many platforms)
	if temps, err := host.SensorsTemperaturesWithContext(ctx); err == nil && len(temps) > 0 {
		var best *float64
		for _, t := range temps {
			k := toLowerASCII(t.SensorKey)
			if contains(k, "package") || contains(k, "tctl") || contains(k, "cpu") || contains(k, "core") {
				val := t.Temperature
				best = &val
				break
			}
		}
		if best != nil {
			return best
		}
	}

	// Platform-specific fallbacks
	switch runtime.GOOS {
	case "linux":
		return getCPUTemperatureLinux()
	case "darwin":
		return getCPUTemperatureMacOS()
	case "windows":
		return getCPUTemperatureWindows()
	}

	return nil
}

func getCPUTemperatureLinux() *float64 {
	// Try common thermal zones
	thermalPaths := []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/class/thermal/thermal_zone1/temp",
		"/sys/devices/platform/coretemp.0/hwmon/hwmon*/temp*_input",
	}

	for _, path := range thermalPaths {
		if matches, _ := filepath.Glob(path); len(matches) > 0 {
			for _, match := range matches {
				if data, err := ioutil.ReadFile(match); err == nil {
					if temp, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
						// Most Linux thermal zones report in millicelsius
						if temp > 1000 {
							temp = temp / 1000.0
						}
						if temp > 0 && temp < 150 { // Sanity check
							return &temp
						}
					}
				}
			}
		}
	}
	return nil
}

func getCPUTemperatureMacOS() *float64 {
	// Try using powermetrics (requires admin) or smc tools
	cmd := exec.Command("powermetrics", "-n", "1", "-i", "1000", "--samplers", "smc", "-f", "plist")
	if output, err := cmd.Output(); err == nil {
		// Parse powermetrics output for CPU temperature
		// This is a simplified parser - you'd want more robust parsing
		if strings.Contains(string(output), "CPU die temperature") {
			// Extract temperature value from plist output
			// Implementation depends on exact output format
		}
	}
	return nil
}

// Enhanced CPU Package Power Collection
func getCPUPackagePower(ctx context.Context) *float64 {
	switch runtime.GOOS {
	case "linux":
		return getCPUPackagePowerLinux()
	case "darwin":
		return getCPUPackagePowerMacOS()
	case "windows":
		return getCPUPackagePowerWindows()
	}
	return nil
}

func getCPUPackagePowerLinux() *float64 {
	// Intel RAPL (Running Average Power Limit)
	raplPaths := []string{
		"/sys/class/powercap/intel-rapl/intel-rapl:0/energy_uj",
		"/sys/devices/virtual/powercap/intel-rapl/intel-rapl:0/energy_uj",
	}

	for _, path := range raplPaths {
		if data, err := ioutil.ReadFile(path); err == nil {
			if energy, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
				// Convert microjoules to watts (would need time diff for accurate measurement)
				// This is a simplified implementation
				watts := energy / 1000000.0 // Convert to joules
				return &watts
			}
		}
	}
	return nil
}

func getCPUPackagePowerMacOS() *float64 {
	// macOS would need powermetrics or IOKit access
	cmd := exec.Command("powermetrics", "-n", "1", "-i", "1000", "--samplers", "cpu_power")
	if output, err := cmd.Output(); err == nil {
		// Parse CPU power from powermetrics output
		lines := strings.SplitSeq(string(output), "\n")
		for line := range lines {
			if strings.Contains(line, "CPU Power") {
				// Extract power value
				// Implementation depends on exact format
			}
		}
	}
	return nil
}

// Enhanced GPU Metrics Collection
func collectGPUMetrics(ctx context.Context) []GPUMetrics {
	var gpus []GPUMetrics

	// Try NVIDIA first
	if nvidiaGPUs := getNVIDIAGPUs(); len(nvidiaGPUs) > 0 {
		gpus = append(gpus, nvidiaGPUs...)
	}

	// Try AMD
	if amdGPUs := getAMDGPUs(); len(amdGPUs) > 0 {
		gpus = append(gpus, amdGPUs...)
	}

	// Try Intel integrated graphics
	if intelGPUs := getIntelGPUs(); len(intelGPUs) > 0 {
		gpus = append(gpus, intelGPUs...)
	}

	return gpus
}

func getNVIDIAGPUs() []GPUMetrics {
	var gpus []GPUMetrics

	// Try nvidia-smi
	cmd := exec.Command("nvidia-smi", "--query-gpu=name,utilization.gpu,temperature.gpu,power.draw,memory.total,memory.used,clocks.gr", "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return gpus
	}

	lines := strings.SplitSeq(strings.TrimSpace(string(output)), "\n")
	for line := range lines {
		fields := strings.Split(line, ", ")
		if len(fields) >= 7 {
			gpu := GPUMetrics{
				Vendor: "NVIDIA",
				Name:   strings.TrimSpace(fields[0]),
			}

			if util, err := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64); err == nil {
				gpu.UtilizationPct = &util
			}
			if temp, err := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64); err == nil {
				gpu.TemperatureC = &temp
			}
			if power, err := strconv.ParseFloat(strings.TrimSpace(fields[3]), 64); err == nil {
				gpu.PowerW = &power
			}
			if memTotal, err := strconv.ParseUint(strings.TrimSpace(fields[4]), 10, 64); err == nil {
				total := memTotal * 1024 * 1024 // Convert MB to bytes
				gpu.MemoryTotalB = &total
			}
			if memUsed, err := strconv.ParseUint(strings.TrimSpace(fields[5]), 10, 64); err == nil {
				used := memUsed * 1024 * 1024 // Convert MB to bytes
				gpu.MemoryUsedB = &used
			}
			if clock, err := strconv.ParseFloat(strings.TrimSpace(fields[6]), 64); err == nil {
				gpu.ClockMHz = &clock
			}

			gpus = append(gpus, gpu)
		}
	}

	return gpus
}

func getAMDGPUs() []GPUMetrics {
	var gpus []GPUMetrics

	// Try rocm-smi for AMD GPUs
	cmd := exec.Command("rocm-smi", "--showuse", "--showtemp", "--showpower", "--showmeminfo", "--csv")
	output, err := cmd.Output()
	if err != nil {
		return gpus
	}

	// Parse rocm-smi CSV output
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return gpus
	}

	// Simple CSV parsing - you'd want more robust parsing
	for i := 1; i < len(lines); i++ {
		fields := strings.Split(lines[i], ",")
		if len(fields) >= 6 {
			gpu := GPUMetrics{
				Vendor: "AMD",
				Name:   "AMD GPU", // rocm-smi might not provide name in this format
			}

			// Parse fields based on rocm-smi output format
			// Implementation depends on exact CSV format

			gpus = append(gpus, gpu)
		}
	}

	return gpus
}

func getIntelGPUs() []GPUMetrics {
	var gpus []GPUMetrics
	if runtime.GOOS == "linux" {
		gpu := GPUMetrics{
			Vendor: "Intel",
			Name:   "Intel Integrated Graphics",
		}
		// Parse the output for metrics
		gpus = append(gpus, gpu)

	}

	return gpus
}

// Enhanced Disk Temperature Collection
func getDiskTemperature(device string) *float64 {
	if device == "" {
		return nil
	}

	switch runtime.GOOS {
	case "linux":
		return getDiskTemperatureLinux(device)
	case "darwin":
		return getDiskTemperatureMacOS(device)
	case "windows":
		return getDiskTemperatureWindows(device)
	}

	return nil
}

func getDiskTemperatureLinux(device string) *float64 {
	// Try smartctl for SMART temperature
	cmd := exec.Command("smartctl", "-A", device)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.SplitSeq(string(output), "\n")
	for line := range lines {
		if strings.Contains(line, "Temperature_Celsius") || strings.Contains(line, "Airflow_Temperature_Cel") {
			fields := strings.Fields(line)
			if len(fields) >= 10 {
				if temp, err := strconv.ParseFloat(fields[9], 64); err == nil {
					return &temp
				}
			}
		}
	}

	// Try hddtemp as fallback
	cmd = exec.Command("hddtemp", "-n", device)
	if output, err := cmd.Output(); err == nil {
		if temp, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64); err == nil {
			return &temp
		}
	}

	return nil
}

func getDiskTemperatureMacOS(device string) *float64 {
	// macOS can use smartctl or diskutil
	cmd := exec.Command("smartctl", "-A", device)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	// Parse smartctl output similar to Linux
	lines := strings.SplitSeq(string(output), "\n")
	for line := range lines {
		if strings.Contains(line, "Temperature_Celsius") {
			fields := strings.Fields(line)
			if len(fields) >= 10 {
				if temp, err := strconv.ParseFloat(fields[9], 64); err == nil {
					return &temp
				}
			}
		}
	}

	return nil
}

// Enhanced System Power Collection
func getSystemPower(ctx context.Context) *float64 {
	switch runtime.GOOS {
	case "linux":
		return getSystemPowerLinux()
	case "darwin":
		return getSystemPowerMacOS()
	case "windows":
		return getSystemPowerWindows()
	}
	return nil
}

func getSystemPowerLinux() *float64 {
	// Try ACPI power supply info
	if data, err := ioutil.ReadFile("/sys/class/power_supply/BAT0/power_now"); err == nil {
		if power, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			// Convert microwatts to watts
			watts := power / 1000000.0
			return &watts
		}
	}

	// Try reading from UPS tools if available
	cmd := exec.Command("upsc", "ups@localhost", "ups.power")
	if output, err := cmd.Output(); err == nil {
		if power, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64); err == nil {
			return &power
		}
	}

	return nil
}

func getSystemPowerMacOS() *float64 {
	// Use powermetrics for system power on macOS
	cmd := exec.Command("powermetrics", "-n", "1", "-i", "1000", "--samplers", "smc")
	if output, err := cmd.Output(); err == nil {
		// Parse powermetrics output for total system power
		// Implementation depends on exact output format
		lines := strings.SplitSeq(string(output), "\n")
		for line := range lines {
			if strings.Contains(line, "System Power") {
				// Extract power value
			}
		}
	}
	return nil
}

func getCPUTemperatureWindows() *float64 {
	return nil
}

func getCPUPackagePowerWindows() *float64 {
	return nil
}

func getSystemPowerWindows() *float64 {
	return nil
}

func getDiskTemperatureWindows(device string) *float64 {
	return nil
}

// -------- helpers (ASCII-only, fast) --------

func toLowerASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
func contains(haystack, needle string) bool {
	return indexFold(haystack, needle) >= 0
}
func indexFold(s, sub string) int {
	n := len(sub)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		ok := true
		for j := range n {
			a := s[i+j]
			b := sub[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		aa, bb := a[i], b[i]
		if 'A' <= aa && aa <= 'Z' {
			aa += 'a' - 'A'
		}
		if 'A' <= bb && bb <= 'Z' {
			bb += 'a' - 'A'
		}
		if aa != bb {
			return false
		}
	}
	return true
}
func mountOfPath(path string, parts []disk.PartitionStat) string {
	if path == "" {
		return ""
	}
	best := ""
	for _, p := range parts {
		mp := p.Mountpoint
		if mp == "" {
			continue
		}
		if runtime.GOOS == "windows" {
			if len(path) >= len(mp) && equalFold(path[:len(mp)], mp) && len(mp) > len(best) {
				best = mp
			}
		} else {
			if len(path) >= len(mp) && path[:len(mp)] == mp && len(mp) > len(best) {
				best = mp
			}
		}
	}
	return best
}
func matchDeviceLoose(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) > 5 && a[:5] == "/dev/" {
		a = a[5:]
	}
	if len(b) > 5 && b[:5] == "/dev/" {
		b = b[5:]
	}
	return a == b
}

// --- HWInfo-like formatting (adds only formatting, not collection logic) ---

type hwInfoItem struct {
	SensorApp        string `json:"SensorApp"`
	SensorClass      string `json:"SensorClass"`
	SensorName       string `json:"SensorName"`
	SensorValue      string `json:"SensorValue"`
	SensorUnit       string `json:"SensorUnit"`
	SensorUpdateTime int64  `json:"SensorUpdateTime"`
}

// ToHWInfoFormat converts a native Snapshot to an array of HWiNFO-like entries.
func ToHWInfoFormat(ctx context.Context, snap Snapshot) ([]hwInfoItem, error) {
	items := make([]hwInfoItem, 0, 256)
	ts := snap.CollectedAt
	sysClass := "System"
	cpuClass := "CPU"
	memClass := "System Memory"
	netClass := "Network"
	diskClass := "Drives"

	// CPU
	if snap.CPU.UtilizationPct != nil {
		items = append(items, hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      cpuClass,
			SensorName:       "Total CPU Usage",
			SensorValue:      f2(*snap.CPU.UtilizationPct),
			SensorUnit:       "%",
			SensorUpdateTime: ts,
		})
	}
	if snap.CPU.ClockMHz != nil {
		items = append(items, hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      cpuClass,
			SensorName:       "Base Clock",
			SensorValue:      f2(*snap.CPU.ClockMHz),
			SensorUnit:       "MHz",
			SensorUpdateTime: ts,
		})
	}
	// Per-core usage (match HWiNFO "Core X Usage" style)
	for i, v := range snap.CPU.PerCorePct {
		items = append(items, hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      cpuClass,
			SensorName:       fmt.Sprintf("Core %d Usage", i),
			SensorValue:      f2(v),
			SensorUnit:       "%",
			SensorUpdateTime: ts,
		})
	}
	if snap.CPU.TemperatureC != nil {
		items = append(items, hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      cpuClass,
			SensorName:       "CPU Temperature",
			SensorValue:      f1(*snap.CPU.TemperatureC),
			SensorUnit:       "°C",
			SensorUpdateTime: ts,
		})
	}
	// Package power
	if snap.CPU.PackagePowerW != nil {
		items = append(items, hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      cpuClass,
			SensorName:       "CPU Package Power",
			SensorValue:      f2(*snap.CPU.PackagePowerW),
			SensorUnit:       "W",
			SensorUpdateTime: ts,
		})
	}

	// Memory – mirror HWiNFO naming
	items = append(items,
		hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      memClass,
			SensorName:       "Physical Memory Used",
			SensorValue:      fmt.Sprintf("%d", bytesToMB(snap.Memory.Used)),
			SensorUnit:       "MB",
			SensorUpdateTime: ts,
		},
		hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      memClass,
			SensorName:       "Physical Memory Available",
			SensorValue:      fmt.Sprintf("%d", bytesToMB(snap.Memory.Free)),
			SensorUnit:       "MB",
			SensorUpdateTime: ts,
		},
		hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      memClass,
			SensorName:       "Physical Memory Load",
			SensorValue:      f1(snap.Memory.UsedPct),
			SensorUnit:       "%",
			SensorUpdateTime: ts,
		},
	)

	// Virtual memory
	items = append(items,
		hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      memClass,
			SensorName:       "Virtual Memory Commited",
			SensorValue:      fmt.Sprintf("%d", bytesToMB(snap.Memory.Used+snap.Memory.Free)),
			SensorUnit:       "MB",
			SensorUpdateTime: ts,
		},
		hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      memClass,
			SensorName:       "Virtual Memory Available",
			SensorValue:      fmt.Sprintf("%d", bytesToMB(snap.Memory.Free)),
			SensorUnit:       "MB",
			SensorUpdateTime: ts,
		},
	)

	// Disks
	for _, d := range snap.Disks {
		mp := d.Mountpoint
		dev := d.Device
		class := diskClass
		if d.IsLiveOutputFS {
			class = "Live Output FS"
		}
		if d.Total != nil {
			items = append(items, hwInfoItem{
				SensorApp:        "Native",
				SensorClass:      class,
				SensorName:       fmt.Sprintf("%s Total Size", labelDisk(mp, dev)),
				SensorValue:      fmt.Sprintf("%d", bytesToGB(*d.Total)),
				SensorUnit:       "GB",
				SensorUpdateTime: ts,
			})
		}
		if d.Free != nil {
			items = append(items, hwInfoItem{
				SensorApp:        "Native",
				SensorClass:      class,
				SensorName:       fmt.Sprintf("%s Free Space", labelDisk(mp, dev)),
				SensorValue:      fmt.Sprintf("%d", bytesToGB(*d.Free)),
				SensorUnit:       "GB",
				SensorUpdateTime: ts,
			})
		}
		if d.UsedPct != nil {
			items = append(items, hwInfoItem{
				SensorApp:        "Native",
				SensorClass:      class,
				SensorName:       fmt.Sprintf("%s Used", labelDisk(mp, dev)),
				SensorValue:      f1(*d.UsedPct),
				SensorUnit:       "%",
				SensorUpdateTime: ts,
			})
		}
		if d.ReadPerSec != nil {
			items = append(items, hwInfoItem{
				SensorApp:        "Native",
				SensorClass:      class,
				SensorName:       fmt.Sprintf("%s Read Rate", labelDisk(mp, dev)),
				SensorValue:      f0(*d.ReadPerSec),
				SensorUnit:       "B/s",
				SensorUpdateTime: ts,
			})
		}
		if d.WritePerSec != nil {
			items = append(items, hwInfoItem{
				SensorApp:        "Native",
				SensorClass:      class,
				SensorName:       fmt.Sprintf("%s Write Rate", labelDisk(mp, dev)),
				SensorValue:      f0(*d.WritePerSec),
				SensorUnit:       "B/s",
				SensorUpdateTime: ts,
			})
		}
		if d.TemperatureC != nil {
			items = append(items, hwInfoItem{
				SensorApp:        "Native",
				SensorClass:      class,
				SensorName:       fmt.Sprintf("%s Temperature", labelDisk(mp, dev)),
				SensorValue:      f1(*d.TemperatureC),
				SensorUnit:       "°C",
				SensorUpdateTime: ts,
			})
		}
	}

	// Network (cumulative)
	items = append(items,
		hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      netClass,
			SensorName:       "Bytes Sent",
			SensorValue:      fmt.Sprintf("%d", snap.Network.BytesSent),
			SensorUnit:       "B",
			SensorUpdateTime: ts,
		},
		hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      netClass,
			SensorName:       "Bytes Received",
			SensorValue:      fmt.Sprintf("%d", snap.Network.BytesRecv),
			SensorUnit:       "B",
			SensorUpdateTime: ts,
		},
	)

	// System power
	if snap.SystemPower != nil {
		items = append(items, hwInfoItem{
			SensorApp:        "Native",
			SensorClass:      sysClass,
			SensorName:       "System Power",
			SensorValue:      f2(*snap.SystemPower),
			SensorUnit:       "W",
			SensorUpdateTime: ts,
		})
	}

	// GPU metrics
	for i, g := range snap.GPU {
		gclass := fmt.Sprintf("GPU %d: %s", i, g.Name)
		if g.UtilizationPct != nil {
			items = append(items, hwInfoItem{"Native", gclass, "GPU Utilization", f1(*g.UtilizationPct), "%", ts})
		}
		if g.ClockMHz != nil {
			items = append(items, hwInfoItem{"Native", gclass, "GPU Clock", f2(*g.ClockMHz), "MHz", ts})
		}
		if g.PowerW != nil {
			items = append(items, hwInfoItem{"Native", gclass, "GPU Power", f2(*g.PowerW), "W", ts})
		}
		if g.TemperatureC != nil {
			items = append(items, hwInfoItem{"Native", gclass, "GPU Temperature", f1(*g.TemperatureC), "°C", ts})
		}
		if g.MemoryTotalB != nil {
			items = append(items, hwInfoItem{"Native", gclass, "GPU Memory Total", fmt.Sprintf("%d", bytesToMB(*g.MemoryTotalB)), "MB", ts})
		}
		if g.MemoryUsedB != nil {
			items = append(items, hwInfoItem{"Native", gclass, "GPU Memory Used", fmt.Sprintf("%d", bytesToMB(*g.MemoryUsedB)), "MB", ts})
		}
	}

	return items, nil
}

// --- tiny format helpers (keep SensorValue as string, like HWiNFO) ---
func f0(v float64) string { return fmt.Sprintf("%.0f", v) }
func f1(v float64) string { return fmt.Sprintf("%.1f", v) }
func f2(v float64) string { return fmt.Sprintf("%.2f", v) }

func bytesToMB(b uint64) uint64 { return b / (1024 * 1024) }
func bytesToGB(b uint64) uint64 { return b / (1024 * 1024 * 1024) }

func labelDisk(mount, dev string) string {
	if mount != "" {
		return mount
	}
	if dev != "" {
		return dev
	}
	return "Disk"
}
