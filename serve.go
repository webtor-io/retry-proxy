package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	s "github.com/webtor-io/retry-proxy/services"
)

func makeServeCMD() cli.Command {
	serveCmd := cli.Command{
		Name:    "serve",
		Aliases: []string{"s"},
		Usage:   "Serves web server",
		Action:  serve,
	}
	configureServe(&serveCmd)
	return serveCmd
}

func configureServe(c *cli.Command) {
	c.Flags = s.RegisterWebFlags([]cli.Flag{})
	c.Flags = cs.RegisterProbeFlags(c.Flags)
}

func serve(c *cli.Context) error {
	// Setting Probe
	probe := cs.NewProbe(c)
	defer probe.Close()

	// Setting Web
	web := s.NewWeb(c)
	defer web.Close()

	// Setting ServeService
	serve := cs.NewServe(probe, web)

	// And SERVE!
	err := serve.Serve()
	if err != nil {
		log.WithError(err).Error("got server error")
	}
	return err
}
