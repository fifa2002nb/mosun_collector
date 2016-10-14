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
		return nil
	}); err != nil {
		Error = err
	}
	Add(&md, osMemUsed, (int(mem["MemTotal"])-(int(mem["MemFree"])+int(mem["Buffers"])+int(mem["Cached"])))*1024, nil, metadata.Gauge, metadata.Bytes, osMemUsedDesc) //内存使用字节数
	if mem["MemTotal"] != 0 && mem["MemFree"] != 0 {
		Add(&md, osMemPctUsed, (mem["MemTotal"]-mem["MemFree"]-mem["Buffers"]-mem["Cached"])/mem["MemTotal"], nil, metadata.Gauge, metadata.Pct, osMemUsedDesc) //内存使用率（不包含buffer和cached）
	}
	num_cores := 0
	var t_util float64
	var t_idle float64
	if err := readLine("/proc/stat", func(s string) error {
		m := statRE.FindStringSubmatch(s)
		if m == nil {
			return nil
		}
		if strings.HasPrefix(m[1], "cpu") {
			cpu_m := statCPURE.FindStringSubmatch(m[1]) //匹配各个单核字段
			fields := strings.Fields(m[2])
			if nil != cpu_m {
				num_cores += 1
			} else { //这里只记录所有cpu核汇总的统计值
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
				idle, err := strconv.ParseFloat(fields[3], 64)
				if err != nil {
					return nil
				}
				iowait, err := strconv.ParseFloat(fields[4], 64)
				if err != nil {
					return nil
				}
				irq, err := strconv.ParseFloat(fields[5], 64)
				if err != nil {
					return nil
				}
				softirq, err := strconv.ParseFloat(fields[6], 64)
				if err != nil {
					return nil
				}
				steal, err := strconv.ParseFloat(fields[7], 64)
				if err != nil {
					return nil
				}
				guest, err := strconv.ParseFloat(fields[8], 64)
				if err != nil {
					return nil
				}
				t_util = user + nice + system + iowait + irq + softirq + steal + guest
				t_idle = idle
			}
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
	if num_cores != 0 && t_util != 0 && t_idle != 0 {
		Add(&md, osCPU+".used", t_util, nil, metadata.Counter, metadata.Pct, "")
		Add(&md, osCPU+".idle", t_idle, nil, metadata.Counter, metadata.Pct, "")
		Add(&md, osCPU+".percent_used", (t_util)/(t_util+t_idle), nil, metadata.Counter, metadata.Pct, "")
	}
	cpuinfo_index := 0
	if err := readLine("/proc/cpuinfo", func(s string) error {
		m := cpuspeedRE.FindStringSubmatch(s)
		if m == nil {
			return nil
		}
		tags := opentsdb.TagSet{"cpu": strconv.Itoa(cpuinfo_index)}
		Add(&md, osCPUClock, m[1], tags, metadata.Gauge, metadata.MHz, osCPUClockDesc)
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
