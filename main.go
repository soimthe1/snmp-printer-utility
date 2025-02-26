package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
)

// Map for printer status codes (from Host Resources MIB)
var printerStatusMap = map[int]string{
	1: "other",
	2: "unknown",
	3: "idle",
	4: "printing",
	5: "warmup",
}

// Supply represents a printer supply (e.g., toner/ink)
type Supply struct {
	Description string
	Level       int
	MaxCapacity int
}

// Tray represents a paper tray
type Tray struct {
	Name         string
	CurrentLevel int
	MaxCapacity  int
}

// incIP increments an IPv4 address
func incIP(ip net.IP) net.IP {
	ip = ip.To4()
	if ip == nil {
		return nil
	}
	newIP := make(net.IP, 4)
	copy(newIP, ip)
	for i := 3; i >= 0; i-- {
		if newIP[i] < 255 {
			newIP[i]++
			return newIP
		}
		newIP[i] = 0
	}
	return newIP
}

// checkSNMP verifies if a device is a printer via SNMP
func checkSNMP(ip string, community string) bool {
	params := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      161,
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   time.Duration(3) * time.Second,
		Retries:   2,
	}

	err := params.Connect()
	if err != nil {
		return false
	}
	defer params.Conn.Close()

	// Check printer status to confirm it's a printer
	oid := ".1.3.6.1.2.1.25.3.5.1.1.1" // hrPrinterStatus.1
	result, err := params.Get([]string{oid})
	if err != nil || len(result.Variables) == 0 || result.Variables[0].Type == gosnmp.NoSuchObject {
		return false
	}

	// Try multiple OIDs for naming
	name := ""
	oids := []string{
		".1.3.6.1.2.1.43.5.1.1.16.1", // prtGeneralPrinterName.1
		".1.3.6.1.2.1.1.1.0",         // sysDescr
		".1.3.6.1.2.1.1.5.0",         // sysName
	}
	nameResult, err := params.Get(oids)
	if err == nil {
		for _, variable := range nameResult.Variables {
			if str, ok := variable.Value.(string); ok && str != "" {
				name = str
				break
			}
		}
	}
	if name != "" {
		fmt.Printf("🎯 Found printer: %s → %s\n", ip, name)
	} else {
		fmt.Printf("🎯 Found printer: %s → (unnamed)\n", ip)
	}
	return true
}

// scanNetwork scans a CIDR range for printers using goroutines
func scanNetwork(cidr string, community string, workers int) []string {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		log.Fatalf("Invalid CIDR: %v", err)
	}

	var printers []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	ipChan := make(chan net.IP, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range ipChan {
				if checkSNMP(ip.String(), community) {
					mu.Lock()
					printers = append(printers, ip.String())
					mu.Unlock()
				}
			}
		}()
	}

	ip := ipnet.IP
	for ipnet.Contains(ip) {
		ipChan <- ip
		ip = incIP(ip)
		if ip == nil {
			break
		}
	}
	close(ipChan)
	wg.Wait()
	return printers
}

// pollPrinter retrieves detailed printer information
func pollPrinter(ip string, community string) {
	params := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      161,
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   time.Duration(3) * time.Second,
		Retries:   2,
	}

	err := params.Connect()
	if err != nil {
		fmt.Printf("❌ Failed to connect to %s: %v\n", ip, err)
		return
	}
	defer params.Conn.Close()

	// Single-value OIDs
	oids := []string{
		".1.3.6.1.2.1.1.1.0",         // sysDescr
		".1.3.6.1.2.1.25.3.5.1.1.1",  // hrPrinterStatus.1
		".1.3.6.1.2.1.43.5.1.1.16.1", // prtGeneralPrinterName.1
		".1.3.6.1.2.1.43.10.2.1.4.1", // prtMarkerLifeCount.1 (total pages)
	}

	result, err := params.Get(oids)
	if err != nil {
		fmt.Printf("❌ SNMP Get error for %s: %v\n", ip, err)
		return
	}

	fmt.Printf("\n🖨️ Printer Report for %s:\n", ip)
	for _, variable := range result.Variables {
		switch variable.Name {
		case ".1.3.6.1.2.1.1.1.0":
			if str, ok := variable.Value.(string); ok && str != "" {
				fmt.Printf("  System Description: %s\n", str)
			}
		case ".1.3.6.1.2.1.43.5.1.1.16.1":
			if str, ok := variable.Value.(string); ok && str != "" {
				fmt.Printf("  Printer Name: %s\n", str)
			}
		case ".1.3.6.1.2.1.25.3.5.1.1.1":
			if val, ok := variable.Value.(int); ok {
				status, exists := printerStatusMap[val]
				if exists {
					fmt.Printf("  Printer Status: %s\n", status)
				} else {
					fmt.Printf("  Printer Status: %d (unknown)\n", val)
				}
			}
		case ".1.3.6.1.2.1.43.10.2.1.4.1":
			if val, ok := variable.Value.(int); ok {
				fmt.Printf("  Total Pages Printed: %d\n", val)
			}
		}
	}

	// Walk prtMarkerSuppliesTable for supplies
	supplies := make(map[int]Supply)
	err = params.Walk(".1.3.6.1.2.1.43.11.1.1", func(variable gosnmp.SnmpPDU) error {
		parts := strings.Split(variable.Name, ".")
		if len(parts) < 2 {
			return nil
		}
		index, _ := strconv.Atoi(parts[len(parts)-1])
		supply, exists := supplies[index]
		if !exists {
			supply = Supply{}
		}
		switch {
		case strings.HasPrefix(variable.Name, ".1.3.6.1.2.1.43.11.1.1.6"):
			if str, ok := variable.Value.(string); ok {
				supply.Description = str
			}
		case strings.HasPrefix(variable.Name, ".1.3.6.1.2.1.43.11.1.1.9"):
			if val, ok := variable.Value.(int); ok {
				supply.Level = val
			}
		case strings.HasPrefix(variable.Name, ".1.3.6.1.2.1.43.11.1.1.8"):
			if val, ok := variable.Value.(int); ok {
				supply.MaxCapacity = val
			}
		}
		supplies[index] = supply
		return nil
	})
	if err == nil && len(supplies) > 0 {
		fmt.Println("  Supplies:")
		for _, supply := range supplies {
			desc := supply.Description
			if desc == "" {
				desc = "Unknown Supply"
			}
			fmt.Printf("    - %s: %d", desc, supply.Level)
			if supply.MaxCapacity > 0 && supply.Level >= 0 {
				percent := (float64(supply.Level) / float64(supply.MaxCapacity)) * 100
				fmt.Printf(" (%d%% of %d)", int(percent), supply.MaxCapacity)
			} else if supply.Level == -3 {
				fmt.Print(" (unknown)")
			}
			fmt.Println()
		}
	} else {
		fmt.Printf("  Supplies: (No data available: %v)\n", err)
	}

	// Walk prtInputTable for paper trays
	trays := make(map[int]Tray)
	err = params.Walk(".1.3.6.1.2.1.43.8.2.1", func(variable gosnmp.SnmpPDU) error {
		parts := strings.Split(variable.Name, ".")
		if len(parts) < 2 {
			return nil
		}
		index, _ := strconv.Atoi(parts[len(parts)-1])
		tray, exists := trays[index]
		if !exists {
			tray = Tray{}
		}
		switch {
		case strings.HasPrefix(variable.Name, ".1.3.6.1.2.1.43.8.2.1.2"):
			if str, ok := variable.Value.(string); ok {
				tray.Name = str
			}
		case strings.HasPrefix(variable.Name, ".1.3.6.1.2.1.43.8.2.1.9"):
			if val, ok := variable.Value.(int); ok {
				tray.CurrentLevel = val
			}
		case strings.HasPrefix(variable.Name, ".1.3.6.1.2.1.43.8.2.1.8"):
			if val, ok := variable.Value.(int); ok {
				tray.MaxCapacity = val
			}
		}
		trays[index] = tray
		return nil
	})
	if err == nil && len(trays) > 0 {
		fmt.Println("  Paper Trays:")
		for _, tray := range trays {
			name := tray.Name
			if name == "" {
				name = "Unknown Tray"
			}
			fmt.Printf("    - %s: %d", name, tray.CurrentLevel)
			if tray.MaxCapacity > 0 && tray.CurrentLevel >= 0 {
				percent := (float64(tray.CurrentLevel) / float64(tray.MaxCapacity)) * 100
				fmt.Printf(" (%d%% of %d)", int(percent), tray.MaxCapacity)
			}
			fmt.Println()
		}
	} else {
		fmt.Printf("  Paper Trays: (No data available: %v)\n", err)
	}
}

func main() {
	cidr := flag.String("cidr", "192.168.1.0/24", "Network CIDR to scan (e.g., 192.168.1.0/24)")
	community := flag.String("community", "public", "SNMP community string")
	workers := flag.Int("workers", 10, "Number of concurrent workers for scanning")
	flag.Parse()

	fmt.Printf("🔎 Scanning network %s with %d workers for SNMP-enabled printers...\n", *cidr, *workers)
	printers := scanNetwork(*cidr, *community, *workers)

	if len(printers) == 0 {
		fmt.Println("❌ No SNMP printers found!")
	} else {
		fmt.Printf("✅ Found %d SNMP printers:\n", len(printers))
		for _, printer := range printers {
			pollPrinter(printer, *community)
		}
	}
}
