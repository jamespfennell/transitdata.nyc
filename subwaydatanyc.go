package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	hconfig "github.com/jamespfennell/hoard/config"
	"github.com/jamespfennell/subwaydata.nyc/etl"
	"github.com/jamespfennell/subwaydata.nyc/etl/config"
	"github.com/jamespfennell/subwaydata.nyc/etl/periodic"
	"github.com/jamespfennell/subwaydata.nyc/etl/storage"
	"github.com/jamespfennell/subwaydata.nyc/metadata"
	"github.com/jamespfennell/subwaydata.nyc/website"
	"github.com/urfave/cli/v2"
)

const (
	etlConfig   = "etl-config"
	hoardConfig = "hoard-config"
)

func main() {
	app := &cli.App{
		Name:     "subwaydatanyc",
		HelpName: "subwaydatanyc",
		Usage:    "tools for the subwaydata.nyc project",
		Commands: []*cli.Command{
			{
				Name:  "etl",
				Usage: "run the subwaydata.nyc ETL pipeline",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     hoardConfig,
						Usage:    "path to the Hoard config file",
						Required: true,
					},
					&cli.StringFlag{
						Name:     etlConfig,
						Usage:    "path to the ETL config file",
						Required: true,
					},
				},
				Subcommands: []*cli.Command{
					{
						Name:  "delete",
						Usage: "delete a range of processed days",
						Flags: []cli.Flag{
							&cli.StringSliceFlag{
								Name:  "day",
								Usage: "day to delete",
							},
							&cli.BoolFlag{
								Name:  "yes",
								Usage: "perform the deletions",
							},
						},
						Action: func(c *cli.Context) error {
							session, err := newSession(c)
							if err != nil {
								return err
							}
							var days []metadata.Day
							for _, rawDay := range c.StringSlice("day") {
								day, err := metadata.ParseDay(rawDay)
								if err != nil {
									return fmt.Errorf("failed to parse start date: %w", err)
								}
								days = append(days, day)
							}
							ctx := context.Background()
							return etl.DeleteDays(ctx, days, !c.Bool("yes"), session.ec, session.sc)
						},
					},
					{
						Name:        "run",
						Usage:       "run the ETL pipeline for a specific day",
						UsageText:   "etl run YYYY-MM-DD",
						Description: "Runs the pipeline for the specified day (YYYY-MM-DD).",
						Action: func(c *cli.Context) error {
							session, err := newSession(c)
							if err != nil {
								return err
							}
							args := c.Args()
							switch args.Len() {
							case 0:
								return fmt.Errorf("no day provided")
							case 1:
								d, err := metadata.ParseDay(args.Get(0))
								if err != nil {
									return err
								}
								return etl.Run(
									context.Background(),
									d,
									// TODO: !!!
									[]string{"nycsubway_L"},
									session.ec,
									session.hc,
									session.sc,
								)
							default:
								return fmt.Errorf("too many command line arguments passed")
							}
						},
					},
					{
						Name:        "backlog",
						Usage:       "run the ETL pipeline for all days that are not up-to-date",
						Description: "Runs the pipeline for days that are not up to date.",
						Flags: []cli.Flag{
							&cli.IntFlag{
								Name:        "limit",
								Aliases:     []string{"l"},
								Usage:       "maximum number of days to process",
								DefaultText: "no limit",
							},
							&cli.IntFlag{
								Name:    "concurrency",
								Aliases: []string{"c"},
								Value:   1,
								Usage:   "number of days to run concurrently",
							},
							&cli.BoolFlag{
								Name:    "dry-run",
								Aliases: []string{"d"},
								Usage:   "only calculate the days that need to be updated, but don't update them",
							},
						},
						Action: func(c *cli.Context) error {
							session, err := newSession(c)
							if err != nil {
								return err
							}
							opts := etl.BacklogOptions{
								DryRun:      c.Bool("dry-run"),
								Concurrency: c.Int("concurrency"),
							}
							if c.IsSet("limit") {
								l := c.Int("limit")
								opts.Limit = &l
							}
							return etl.Backlog(context.Background(), session.ec, session.hc, session.sc, opts)
						},
					},
					{
						Name:  "periodic",
						Usage: "run the ETL pipeline periodically",
						Action: func(c *cli.Context) error {
							session, err := newSession(c)
							if err != nil {
								return err
							}
							args := c.Args().Slice()
							if len(args) == 0 {
								return fmt.Errorf("no intervals provided")
							}
							var intervals []periodic.Interval
							for _, arg := range args {
								interval, err := periodic.NewInterval(arg)
								if err != nil {
									return fmt.Errorf("failed to parse interval: %w", err)
								}
								intervals = append(intervals, interval)
							}
							ctx := context.Background()
							periodic.Run(ctx, session.ec, session.hc, session.sc, intervals)
							return nil
						},
					},
				},
			},
			{
				Name:  "website",
				Usage: "serve the subwaydata.nyc website",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:        "port",
						Usage:       "port to run the HTTP server on",
						Value:       8080,
						DefaultText: "8080",
					},
					&cli.StringFlag{
						Name:     "metadata-url",
						Usage:    "URL for the metadata",
						Required: true,
					},
				},
				Action: func(ctx *cli.Context) error {
					website.Run(ctx.String("metadata-url"), ctx.Int("port"))
					return nil
				},
			},
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

type session struct {
	ec *config.Config
	hc *hconfig.Config
	sc *storage.Client
}

func newSession(c *cli.Context) (*session, error) {
	path := c.String(hoardConfig)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read the Hoard config file from disk: %w", err)
	}
	hc, err := hconfig.NewConfig(b)
	if err != nil {
		return nil, err
	}

	path = c.String(etlConfig)
	b, err = os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read the ETL config file from disk: %w", err)
	}
	var ec config.Config
	if err := json.Unmarshal(b, &ec); err != nil {
		return nil, fmt.Errorf("failed to parse the ETL config file: %w", err)
	}

	sc, err := storage.NewClient(&ec)
	if err != nil {
		return nil, err
	}
	return &session{
		ec: &ec,
		hc: hc,
		sc: sc,
	}, nil
}
