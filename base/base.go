package base

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"mosun_collector/collect"
	"mosun_collector/collector/collectors"
	"mosun_collector/collector/conf"
	"mosun_collector/metadata"
	"mosun_collector/opentsdb"
	"mosun_collector/util"
	"mosun_collector/version"
)

var (
	mains []func()
)

//列出所有采集器类型
func List(c *cli.Context) {
	var (
		b []string
	)
	cs := collectors.Search(b)
	if len(cs) == 0 {
		log.Fatal("no collectors found.")
	}
	list(cs)
}

//转换配置
func ToToml(c *cli.Context) {
	var (
		fname string
		sname string
	)
	if !c.IsSet("totoml") && !c.IsSet("T") {
		log.Fatal("no filename specified.")
	}
	if c.IsSet("totoml") {
		fname = c.String("totoml")
	} else if c.IsSet("T") {
		fname = c.String("T")
	}
	if !c.IsSet("conf") && !c.IsSet("C") {
		log.Fatal("no configure file sepcifed.")
	} else if c.IsSet("conf") {
		sname = c.String("conf")
	} else if c.IsSet("C") {
		sname = c.String("C")
	}

	toToml(sname, fname)
}

//主启动方法
func Start(c *cli.Context) {
	for _, m := range mains {
		m()
	}

	conf := readConf(c)
	if c.IsSet("host") {
		conf.Host = c.String("host")
	} else if c.IsSet("H") {
		conf.Host = c.String("H")
	}
	if c.IsSet("schedhost") {
		conf.SchedHost = c.String("schedhost")
	} else if c.IsSet("S") {
		conf.SchedHost = c.String("S")
	}
	if c.IsSet("license") {
		conf.License = c.String("license")
	} else if c.IsSet("L") {
		conf.License = c.String("L")
	}
	if c.IsSet("filter") {
		conf.Filter = c.StringSlice("filter")
	} else if c.IsSet("I") {
		conf.Filter = c.StringSlice("I")
	}
	if !conf.Tags.Valid() {
		log.Fatalf("invalid tags: %v", conf.Tags)
	} else if conf.Tags["host"] != "" {
		log.Fatalf("host not supported in custom tags, use Hostname instead")
	}
	if conf.PProf != "" {
		go func() {
			log.Infof("Starting pprof at http://%s/debug/pprof/", conf.PProf)
			log.Fatal(http.ListenAndServe(conf.PProf, nil))
		}()
	}
	collectors.AddTags = conf.Tags
	collectors.License = conf.License // add by xuye 20160526
	collect.License = conf.License    // add by xuye 20160526
	util.FullHostname = true
	util.Set()
	if conf.Hostname != "" {
		util.Hostname = conf.Hostname
		if err := collect.SetHostname(conf.Hostname); err != nil {
			log.Fatal(err)
		}
	}
	if conf.ColDir != "" { //外部程序监控
		collectors.InitPrograms(conf.ColDir)
	}
	var err error
	check := func(e error) {
		if e != nil {
			err = e
		}
	}
	for _, cfg := range conf.SNMP { //snmp协议的metric监控，网卡出入包/流量等
		check(collectors.SNMP(cfg, conf.MIBS))
	}

	for _, h := range conf.HTTPUnit {
		if h.TOML != "" {
			check(collectors.HTTPUnitTOML(h.TOML))
		}
		if h.Hiera != "" {
			check(collectors.HTTPUnitHiera(h.Hiera))
		}
	}
	if err != nil {
		log.Fatal(err)
	}
	// Add all process collectors. This is platform specific.
	collectors.WatchProcesses()
	if c.IsSet("fake") {
		collectors.InitFake(c.Int("fake"))
	} else if c.IsSet("F") {
		collectors.InitFake(c.Int("F"))
	}
	collect.DisableDefaultCollectors = conf.DisableSelf
	cs := collectors.Search(conf.Filter)
	if len(cs) == 0 {
		log.Fatalf("Filter %v matches no collectors.", conf.Filter)
	}
	for _, col := range cs {
		col.Init()
	}
	u, err := parseHost(conf.Host)
	su, _ := parseHost(conf.SchedHost) // add by xuye 20160525

	freq := time.Second * time.Duration(conf.Freq)
	if freq <= 0 {
		log.Fatal("freq must be > 0")
	}
	collectors.DefaultFreq = freq
	collect.Freq = freq
	if conf.BatchSize < 0 {
		log.Fatal("BatchSize must be > 0")
	}
	if conf.BatchSize != 0 {
		collect.BatchSize = conf.BatchSize
	}
	collect.Tags = opentsdb.TagSet{"os": runtime.GOOS}
	if c.IsSet("print") {
		collect.Print = c.Bool("print")
	} else if c.IsSet("P") {
		collect.Print = c.Bool("P")
	}
	if !c.IsSet("dismetadata") && !c.IsSet("M") {
		log.Debug(u.String())
		if err := metadata.Init(su); err != nil {
			log.Fatal(err)
		}
	}

	cdp := collectors.Run(cs)
	if u != nil {
		log.Infoln("OpenTSDB host:", u)
	}
	if err := collect.InitChan(u, "collector", cdp); err != nil {
		log.Fatal(err)
	}

	if version.VersionDate != "" {
		v, err := strconv.ParseInt(version.VersionDate, 10, 64)
		if err == nil {
			go func() {
				metadata.AddMetricMeta("collector.version", metadata.Gauge, metadata.None,
					"collector version number, which indicates when collector was built.")
				for {
					if err := collect.Put("version", collect.Tags, v); err != nil {
						log.Error(err)
					}
					time.Sleep(time.Hour)
				}
			}()
		}
	}
	if c.IsSet("batchsize") {
		collect.BatchSize = c.Int("batchsize")
	} else if c.IsSet("B") {
		collect.BatchSize = c.Int("B")
	}
	go func() {
		const maxMem = 500 * 1024 * 1024 // 500MB
		var m runtime.MemStats
		for range time.Tick(time.Minute) {
			runtime.ReadMemStats(&m)
			if m.Alloc > maxMem {
				panic("memory max reached")
			}
		}
	}()
	select {}
}

func list(cs []collectors.Collector) {
	for _, c := range cs {
		fmt.Println(c.Name())
	}
}

//读取配置转换成conf结构体
func readConf(c *cli.Context) *conf.Conf {
	var (
		loc string
	)
	defaultSnmp := conf.SNMP{
		Community: "public",
		Host:      "127.0.0.1",
	}
	conf := &conf.Conf{
		Freq: 10,
	}
	if c.IsSet("conf") {
		loc = c.String("conf")
	} else if c.IsSet("C") {
		loc = c.String("C")
	} else {
		p, err := exePath()
		if err != nil {
			log.Error(err)
			return conf
		}
		dir := filepath.Dir(p)
		loc = filepath.Join(dir, "collector.toml")
	}
	f, err := os.Open(loc)
	if err != nil {
		if c.IsSet("conf") || c.IsSet("C") {
			log.Fatal(err)
		}
		log.Debug(err)
	} else {
		defer f.Close()
		md, err := toml.DecodeReader(f, conf)
		if err != nil {
			log.Fatal(err)
		}
		if u := md.Undecoded(); len(u) > 0 {
			log.Fatalf("extra keys in %s: %v", loc, u)
		}
	}
	// add by xuye 20160526
	if 0 == len(conf.SNMP) {
		conf.SNMP = append(conf.SNMP, defaultSnmp)
	}
	return conf
}

func exePath() (string, error) {
	prog := os.Args[0]
	p, err := filepath.Abs(prog)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(p)
	if err == nil {
		if !fi.Mode().IsDir() {
			return p, nil
		}
		err = fmt.Errorf("%s is directory", p)
	}
	if filepath.Ext(p) == "" {
		p += ".exe"
		fi, err := os.Stat(p)
		if err == nil {
			if !fi.Mode().IsDir() {
				return p, nil
			}
			err = fmt.Errorf("%s is directory", p)
		}
	}
	return "", err
}

func parseHost(host string) (*url.URL, error) {
	if !strings.Contains(host, "//") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, fmt.Errorf("no host specified")
	}
	return u, nil
}

func printPut(c chan *opentsdb.DataPoint) {
	for dp := range c {
		b, _ := json.Marshal(dp)
		log.Info(string(b))
	}
}

//配置文件格式化成toml文件
func toToml(sname, fname string) {
	var c conf.Conf

	b, err := ioutil.ReadFile(sname)
	if err != nil {
		log.Fatal(err)
	}
	extra := new(bytes.Buffer)
	for i, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		sp := strings.SplitN(line, "=", 2)
		if len(sp) != 2 {
			log.Fatalf("expected = in %v:%v", sname, i+1)
		}
		k := strings.TrimSpace(sp[0])
		v := strings.TrimSpace(sp[1])
		switch k {
		case "host":
			c.Host = v
		case "hostname":
			c.Hostname = v
		case "filter":
			c.Filter = strings.Split(v, ",")
		case "coldir":
			c.ColDir = v
		case "snmp":
			for _, s := range strings.Split(v, ",") {
				sp := strings.Split(s, "@")
				if len(sp) != 2 {
					log.Fatal("invalid snmp string:", v)
				}
				c.SNMP = append(c.SNMP, conf.SNMP{
					Community: sp[0],
					Host:      sp[1],
				})
			}
		case "tags":
			tags, err := opentsdb.ParseTags(v)
			if err != nil {
				log.Fatal(err)
			}
			c.Tags = tags
		case "freq":
			freq, err := strconv.Atoi(v)
			if err != nil {
				log.Fatal(err)
			}
			c.Freq = freq
		case "process":
			if runtime.GOOS == "linux" {
				var p struct {
					Command string
					Name    string
					Args    string
				}
				sp := strings.Split(v, ",")
				if len(sp) > 1 {
					p.Name = sp[1]
				}
				if len(sp) > 2 {
					p.Args = sp[2]
				}
				p.Command = sp[0]
				extra.WriteString(fmt.Sprintf(`
[[Process]]
  Command = %q
  Name = %q
  Args = %q
`, p.Command, p.Name, p.Args))
			} else if runtime.GOOS == "windows" {

				extra.WriteString(fmt.Sprintf(`
[[Process]]
  Name = %q
`, v))
			}
		default:
			log.Fatalf("unknown key in %v:%v", sname, i+1)
		}
	}

	f, err := os.Create(fname)
	if err != nil {
		log.Fatal(err)
	}
	if err := toml.NewEncoder(f).Encode(&c); err != nil {
		log.Fatal(err)
	}
	if _, err := extra.WriteTo(f); err != nil {
		log.Fatal(err)
	}
	f.Close()
}
