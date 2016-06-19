package main

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	base "mosun_collector/base"
	"mosun_collector/version"
	"os"
	"path"
)

func main() {
	app := cli.NewApp()
	app.Name = path.Base(os.Args[0])
	app.Usage = "data collector"
	app.Version = version.GetVersionInfo("collector")
	app.Author = ""
	app.Email = ""
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:   "debug",
			Usage:  "debug mode",
			EnvVar: "DEBUG",
		},

		cli.StringFlag{
			Name:  "log-level, l",
			Value: "info",
			Usage: fmt.Sprintf("Log level (options: debug, info, warn, error, fatal, panic)"),
		},
	}

	// logs
	app.Before = func(c *cli.Context) error {
		log.SetOutput(os.Stderr)
		level, err := log.ParseLevel(c.String("log-level"))
		if err != nil {
			log.Fatalf(err.Error())
		}
		log.SetLevel(level)

		// If a log level wasn't specified and we are running in debug mode,
		// enforce log-level=debug.
		if !c.IsSet("log-level") && !c.IsSet("l") && c.Bool("debug") {
			log.SetLevel(log.DebugLevel)
		}
		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:      "list",
			ShortName: "l",
			Usage:     "List available collectors.",
			Action:    base.List,
		},
		{
			Name:      "utils",
			ShortName: "u",
			Usage:     "useful utils.",
			Flags:     []cli.Flag{base.FlToToml, base.FlConf},
			Action:    base.ToToml,
		},
		{
			Name:      "start",
			ShortName: "s",
			Usage:     "run data-collector",
			Flags:     []cli.Flag{base.FlHost, base.FlSchedHost, base.Fllicense, base.FlFilter, base.FlPrint, base.FlBatchSize, base.FlFake, base.FlDisableMetadata, base.FlConf},
			Action:    base.Start,
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
