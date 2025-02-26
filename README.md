# SNMP Printer Utility

A Go-based command-line tool to scan and monitor SNMP-enabled printers on a network. This utility discovers printers, retrieves their status, and reports details like supply levels (e.g., toner) and paper tray capacities using SNMP v2c.

![Go](https://img.shields.io/badge/Go-1.21-blue.svg)
![License](https://img.shields.io/badge/License-MIT-green.svg)
![GitHub Issues](https://img.shields.io/github/issues/soimthe1/snmp-printer-utility)

## Features

- **Network Scanning**: Scans a specified CIDR range for SNMP-enabled printers using concurrent workers.
- **Printer Details**: Retrieves printer status, supply levels (toner/ink), paper tray levels, and total pages printed.
- **Customizable**: Configurable via command-line flags for CIDR, SNMP community string, and worker count.
- **Robust**: Handles timeouts and partial SNMP support gracefully.

## Installation

### Prerequisites

- [Go](https://golang.org/dl/) (version 1.21 or later)
- Git

### Steps

1. **Clone the repository**:
   ```bash
   git clone https://github.com/soimthe1/snmp-printer-utility.git
   cd snmp-printer-utility
2. **Install Dependencies**: Ensure you have Go installed, then download the required Go modules:
   ```bash
   go mod download
3. **Build the binary**: Compile the tool into an executable:
   ```bash
   go build -o snmp-printer-utility

## Usage
Once built, run the utility with the following command:
```bash
./snmp-printer-utility -cidr 192.168.1.0/24 -community public -workers 10
