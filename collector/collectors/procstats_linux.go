package collectors

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"mosun_collector/metadata"
	"mosun_collector/opentsdb"
)

func init() {
	collectors = append(collectors, &IntervalCollector{F: c_procstats_linux})
}

var uptimeRE = regexp.MustCompile(`(\S+)\s+(\S+)`)
var meminfoRE = regexp.MustCompile(`(\w+):\s+(\d+)\s+(\w+)`)
var vmstatRE = regexp.MustCompile(`(\w+)\s+(\d+)`)
var statRE = regexp.MustCompile(`(\w+)\s+(.*)`)
var statCPURE = regexp.MustCompile(`cpu(\d+)`)
var cpuspeedRE = regexp.MustCompile(`cpu MHz\s+: ([\d.]+)`)
var loadavgRE = regexp.MustCompile(`(\S+)\s+(\S+)\s+(\S+)\s+(\d+)/(\d+)\s+`)
var inoutRE = regexp.MustCompile(`(.*)(in|out)`)

var CPU_FIELDS = []string{
	"user",
	"nice",
	"system",
	"idle",
	"iowait",
	"irq",
	"softirq",
	"steal",
	"guest",
	"guest_nice",
}

var NET_STATS_FIELDS = map[string]bool{
	"currestab":    true,
	"indatagrams":  true,
	"outdatagrams": true,
	"passiveopens": true,
	"tcp":          true,
	"udp":          true,
}

func c_procstats_linux() (opentsdb.MultiDataPoint, error) {
	var md opentsdb.MultiDataPoint
	var Error error
	mem := make(map[string]float64)
	if err := readLine("/proc/meminfo", func(s string) error {
		m := meminfoRE.FindStringSubmatch(s)
		if m == nil {
			return nil
		}
		i, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			return err
		}
		mem[m[1]] = i
		//Add(&md, "linux.mem."+strings.ToLower(m[1]), m[2], nil, metadata.Gauge, metadata.KBytes, "")
		return nil
	}); err != nil {
		Error = err
	}
	Add(&md, osMemTotal, int(mem["MemTotal"])*1024, nil, metadata.Gauge, metadata.Bytes, osMemTotalDesc)
	Add(&md, osMemFree, int(mem["MemFree"])*1024, nil, metadata.Gauge, metadata.Bytes, osMemFreeDesc)
	Add(&md, osMemUsed, (int(mem["MemTotal"])-(int(mem["MemFree"])+int(mem["Buffers"])+int(mem["Cached"])))*1024, nil, metadata.Gauge, metadata.Bytes, osMemUsedDesc)
	if mem["MemTotal"] != 0 {
		Add(&md, osMemPctFree, (mem["MemFree"]+mem["Buffers"]+mem["Cached"])/mem["MemTotal"]*100, nil, metadata.Gauge, metadata.Pct, osMemFreeDesc)
	}
	num_cores := 0
	var t_util float64
	cpu_stat_desc := map[string]string{
		"user":       "Normal processes executing in user mode.",
		"nice":       "Niced processes executing in user mode.",
		"system":     "Processes executing in kernel mode.",
		"idle":       "Twiddling thumbs.",
		"iowait":     "Waiting for I/O to complete.",
		"irq":        "Servicing interrupts.",
		"softirq":    "Servicing soft irqs.",
		"steal":      "Involuntary wait.",
		"guest":      "Running a guest vm.",
		"guest_nice": "Running a niced guest vm.",
	}
	if err := readLine("/proc/stat", func(s string) error {
		m := statRE.FindStringSubmatch(s)
		if m == nil {
			return nil
		}
		if strings.HasPrefix(m[1], "cpu") {
			metric_percpu := ""
			tag_cpu := ""
			cpu_m := statCPURE.FindStringSubmatch(m[1])
			if cpu_m != nil {
				num_cores += 1
				metric_percpu = ".percpu"
				tag_cpu = cpu_m[1]
			}
			fields := strings.Fields(m[2])
			for i, value := range fields {
				if i >= len(CPU_FIELDS) {
					break
				}
				tags := opentsdb.TagSet{
					"type": CPU_FIELDS[i],
				}
				if tag_cpu != "" {
					tags["cpu"] = tag_cpu
				}
				Add(&md, "linux.cpu"+metric_percpu, value, tags, metadata.Counter, metadata.CHz, cpu_stat_desc[CPU_FIELDS[i]])
			}
			if metric_percpu == "" {
				if len(fields) < 3 {
					return nil
				}
				user, err := strconv.ParseFloat(fields[0], 64)
				if err != nil {
					return nil
				}
				nice, err := strconv.ParseFloat(fields[1], 64)
				if err != nil {
					return nil
				}
				system, err := strconv.ParseFloat(fields[2], 64)
				if err != nil {
					return nil
				}
				t_util = user + nice + system
			}
		} else if m[1] == "intr" {
			Add(&md, "linux.intr", strings.Fields(m[2])[0], nil, metadata.Counter, metadata.Interupt, "")
		} else if m[1] == "ctxt" {
			Add(&md, "linux.ctxt", m[2], nil, metadata.Counter, metadata.ContextSwitch, "")
		} else if m[1] == "processes" {
			Add(&md, "linux.processes", m[2], nil, metadata.Counter, metadata.Process,
				"The number  of processes and threads created, which includes (but  is not limited  to) those  created by  calls to the  fork() and clone() system calls.")
		} else if m[1] == "procs_blocked" {
			Add(&md, "linux.procs_blocked", m[2], nil, metadata.Gauge, metadata.Process, "The  number of  processes currently blocked, waiting for I/O to complete.")
		}
		return nil
	}); err != nil {
		Error = err
	}
	if num_cores != 0 && t_util != 0 {
		Add(&md, osCPU, t_util/float64(num_cores), nil, metadata.Counter, metadata.Pct, "")
	}
	cpuinfo_index := 0
	if err := readLine("/proc/cpuinfo", func(s string) error {
		m := cpuspeedRE.FindStringSubmatch(s)
		if m == nil {
			return nil
		}
		tags := opentsdb.TagSet{"cpu": strconv.Itoa(cpuinfo_index)}
		Add(&md, osCPUClock, m[1], tags, metadata.Gauge, metadata.MHz, osCPUClockDesc)
		Add(&md, "linux.cpu.clock", m[1], tags, metadata.Gauge, metadata.MHz, osCPUClockDesc)
		cpuinfo_index += 1
		return nil
	}); err != nil {
		Error = err
	}
	if err := readLine("/proc/loadavg", func(s string) error {
		m := loadavgRE.FindStringSubmatch(s)
		if m == nil {
			return nil
		}
		Add(&md, "linux.loadavg_1_min", m[1], nil, metadata.Gauge, metadata.Load, "")
		Add(&md, "linux.loadavg_5_min", m[2], nil, metadata.Gauge, metadata.Load, "")
		Add(&md, "linux.loadavg_15_min", m[3], nil, metadata.Gauge, metadata.Load, "")
		Add(&md, "linux.loadavg_runnable", m[4], nil, metadata.Gauge, metadata.Process, "")
		Add(&md, "linux.loadavg_total_threads", m[5], nil, metadata.Gauge, metadata.Process, "")
		return nil
	}); err != nil {
		Error = err
	}
	irq_type_desc := map[string]string{
		"NMI": "Non-maskable interrupts.",
		"LOC": "Local timer interrupts.",
		"SPU": "Spurious interrupts.",
		"PMI": "Performance monitoring interrupts.",
		"IWI": "IRQ work interrupts.",
		"RES": "Rescheduling interrupts.",
		"CAL": "Funcation call interupts.",
		"TLB": "TLB (translation lookaside buffer) shootdowns.",
		"TRM": "Thermal event interrupts.",
		"THR": "Threshold APIC interrupts.",
		"MCE": "Machine check exceptions.",
		"MCP": "Machine Check polls.",
	}
	num_cpus := 0
	if err := readLine("/proc/interrupts", func(s string) error {
		cols := strings.Fields(s)
		if num_cpus == 0 {
			num_cpus = len(cols)
			return nil
		} else if len(cols) < 2 {
			return nil
		}
		irq_type := strings.TrimRight(cols[0], ":")
		if !IsAlNum(irq_type) {
			return nil
		}
		if IsDigit(irq_type) {
			if cols[len(cols)-2] == "PCI-MSI-edge" && strings.Contains(cols[len(cols)-1], "eth") {
				irq_type = cols[len(cols)-1]
			} else {
				// Interrupt type is just a number, ignore.
				return nil
			}
		}
		for i, val := range cols[1:] {
			if i >= num_cpus || !IsDigit(val) {
				// All values read, remaining cols contain textual description.
				break
			}
			Add(&md, "linux.interrupts", val, opentsdb.TagSet{"type": irq_type, "cpu": strconv.Itoa(i)}, metadata.Counter, metadata.Interupt, irq_type_desc[irq_type])
		}
		return nil
	}); err != nil {
		Error = err
	}
	ln := 0
	var headers []string
	ln = 0
	if err := readLine("/proc/net/snmp", func(s string) error {
		ln++
		if ln%2 != 0 {
			f := strings.Fields(s)
			if len(f) < 2 {
				return fmt.Errorf("Failed to parse header line")
			}
			headers = f
		} else {
			values := strings.Fields(s)
			if len(values) != len(headers) {
				return fmt.Errorf("Mismatched header and value length")
			}
			proto := strings.ToLower(strings.TrimSuffix(values[0], ":"))
			if _, ok := NET_STATS_FIELDS[proto]; ok {
				for i, v := range values {
					if i == 0 {
						continue
					}
					var stype metadata.RateType = metadata.Counter
					stat := strings.ToLower(headers[i])
					if strings.HasPrefix(stat, "rto") {
						stype = metadata.Gauge
					}
					if _, ok1 := NET_STATS_FIELDS[stat]; ok1 {
						Add(&md, "linux.net.stat."+proto+"."+stat, v, nil, stype, metadata.None, "")
					}
				}
			}
		}
		return nil
	}); err != nil {
		Error = err
	}
	// TODO: Bonding monitoring for CentOS 7 using /var/run/teamd/* and teamdctl <team0> state
	if err := readLine("/proc/sys/fs/file-nr", func(s string) error {
		f := strings.Fields(s)
		if len(f) != 3 {
			return fmt.Errorf("unexpected number of fields")
		}
		v, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			return err
		}
		Add(&md, "linux.fs.open", v, nil, metadata.Gauge, metadata.Count, "The number of files presently open.")
		return nil
	}); err != nil {
		Error = err
	}
	return md, Error
}
