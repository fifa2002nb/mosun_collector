package base

import (
	"github.com/codegangsta/cli"
)

var (
	FlHost = cli.StringFlag{
		Name:  "host, H",
		Value: "",
		Usage: "OpenTSDB or Bosun host to send data. Overrides Host in conf file.",
	}

	FlSchedHost = cli.StringFlag{
		Name:  "schedhost, S",
		Value: "",
		Usage: "Schedule host to send metadata. Overrides Host in conf file.",
	}
	Fllicense = cli.StringFlag{
		Name:  "license, L",
		Value: "",
		Usage: "License.",
	}

	flFilterValue = cli.StringSlice([]string{""})
	FlFilter      = cli.StringSliceFlag{
		Name:  "filter, I",
		Value: &flFilterValue,
		Usage: "Filters collectors matching these terms, separated by comma. Overrides Filter in conf file.",
	}

	/*FlList = cli.BoolFlag{
	    Name: "list, L",
	    Usage: "List available collectors.",
	}*/

	FlPrint = cli.BoolFlag{
		Name:  "print, P",
		Usage: "Print to screen instead of sending to a host",
	}

	FlBatchSize = cli.IntFlag{
		Name:  "batchsize, B",
		Value: 0,
		Usage: "OpenTSDB batch size. Default is 500.",
	}

	FlFake = cli.IntFlag{
		Name:  "fake, F",
		Value: 0,
		Usage: "Generates X fake data points on the test.fake metric per second.",
	}

	/*FlDebug = cli.BoolFlag{
	    Name: "debug, D",
	    Usage: "Enables debug output.",
	}*/

	FlDisableMetadata = cli.BoolFlag{
		Name:  "dismetadata, M",
		Usage: "Disable sending of metadata.",
	}

	/*FlVersion = cli.BoolFlag{
	    Name: "version, V",
	    Usage: "Prints the version and exits.",
	}*/

	FlConf = cli.StringFlag{
		Name:  "conf, C",
		Value: "",
		Usage: "Location of configuration file. Defaults to scollector.toml in directory of the scollector executable.",
	}

	FlToToml = cli.StringFlag{
		Name:  "totoml, T",
		Value: "",
		Usage: "Location of destination toml file to convert. Reads from value of -conf.",
	}
)
