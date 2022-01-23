package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	hconfig "github.com/jamespfennell/hoard/config"
	"github.com/jamespfennell/subwaydata.nyc/etl/config"
	"github.com/jamespfennell/subwaydata.nyc/etl/git"
	"github.com/jamespfennell/subwaydata.nyc/etl/pipeline"
	"github.com/jamespfennell/subwaydata.nyc/metadata"
	"github.com/urfave/cli/v2"
)

const hoardConfig = "hoard-config"
const etlConfig = "etl-config"

const descriptionMain = `
ETL pipeline for subwaydata.nyc
`

func main() {
	app := &cli.App{
		Name:        "Subway Data NYC ETL Pipeline",
		Usage:       "",
		Description: descriptionMain,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  hoardConfig,
				Usage: "path to the Hoard config file",
			},
			&cli.StringFlag{
				Name:  etlConfig,
				Usage: "path to the ETL config file",
			},
		},
		Commands: []*cli.Command{
			{
				// periodic 01:00:00-04:00:00 05:00:00-06:30:00 - run the backlog process during the day
				// backlog --limit N --timeout T --list-only
				Name:        "run",
				Usage:       "run the ETL pipeline for a specific day",
				UsageText:   "etl run YYYY-MM-DD",
				Description: "Runs the pipeline for the specified day (YYYY-MM-DD).",
				Action: func(c *cli.Context) error {
					hc, err := getHoardConfig(c)
					if err != nil {
						return err
					}
					ec, err := getEtlConfig(c)
					if err != nil {
						return err
					}
					gitSession, err := newGitSession(ec)
					if err != nil {
						return err
					}
					defer gitSession.Close()
					args := c.Args()
					switch args.Len() {
					case 0:
						return fmt.Errorf("no day provided")
					case 1:
						d, err := metadata.ParseDay(args.Get(0))
						if err != nil {
							return err
						}
						return pipeline.Run(
							gitSession,
							d,
							[]string{"nycsubway_L"},
							ec,
							hc,
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
				Action: func(c *cli.Context) error {
					hc, err := getHoardConfig(c)
					if err != nil {
						return err
					}
					_ = hc
					ec, err := getEtlConfig(c)
					if err != nil {
						return err
					}
					gitSession, err := newGitSession(ec)
					if err != nil {
						return err
					}
					defer gitSession.Close()
					m, err := gitSession.ReadMetadata()
					if err != nil {
						return err
					}

					loc, err := time.LoadLocation(ec.Timezone)
					if err != nil {
						return fmt.Errorf("unable to load timezone %q: %w", ec.Timezone, err)
					}
					now := time.Now().In(loc).Add(-5 * time.Hour).Format("2006-01-02")
					d, _ := metadata.ParseDay(now)

					pendingDays := metadata.CalculatePendingDays(m, d)
					if len(pendingDays) == 0 {
						fmt.Println("No days in the backlog")
						return nil
					}
					fmt.Printf("%d days in the backlog:\n", len(pendingDays))
					for i := 0; i < len(pendingDays); i++ {
						if i >= 20 {
							fmt.Printf("...and %d more days\n", len(pendingDays)-20)
							break
						}
						d := pendingDays[len(pendingDays)-i-1]
						fmt.Printf("- %s (feeds: %s)\n", d.Day, d.FeedIDs)
					}
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

func getHoardConfig(c *cli.Context) (*hconfig.Config, error) {
	if !c.IsSet(hoardConfig) {
		return nil, fmt.Errorf("a Hoard config must be provided")
	}
	path := c.String(hoardConfig)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read the Hoard config file from disk: %w", err)
	}
	return hconfig.NewConfig(b)
}

func getEtlConfig(c *cli.Context) (*config.Config, error) {
	if !c.IsSet(etlConfig) {
		return nil, fmt.Errorf("an ETL config must be provided")
	}
	path := c.String(etlConfig)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read the ETL config file from disk: %w", err)
	}
	var ec config.Config
	if err := json.Unmarshal(b, &ec); err != nil {
		return nil, fmt.Errorf("failed to parse the ETL config file: %w", err)
	}
	return &ec, nil
}

func newGitSession(ec *config.Config) (*git.Session, error) {
	return git.NewWritableSession(
		ec.GitUrl, ec.GitUser, ec.GitPassword, ec.GitEmail, ec.GitBranch, ec.MetadataPath)
}
