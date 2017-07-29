package command

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/apcera/termtables"
	"github.com/araddon/qlbridge/datasource"
	"github.com/mitchellh/cli"

	lytics "github.com/lytics/go-lytics"
)

var globalHelp = `
Global Options:
  --key=xyz         reads LIOKEY envvar, or pass ass command line api key
  --datakey=xyz     Data Api Key, Reads LIODATAKEY envvar as default
  --format=table    [json,csv,table] output as tabular data?  csv?  json?
  --aid=aid         Account id shortcut
`

type commandList func(api *apiCommand) map[string]*command

func Commands(ui cli.Ui) map[string]cli.CommandFactory {

	api := &apiCommand{ui: ui}
	api.f = flag.NewFlagSet("lytics", flag.ContinueOnError)

	topLevelCommands := map[string]commandList{
		"account": accountCommands,
	}

	cmds := make(map[string]cli.CommandFactory)

	for cmd, cmdList := range topLevelCommands {
		for subCmdName, subCmd := range cmdList(api) {
			sub := subCmd
			cmds[fmt.Sprintf("%s %s", cmd, subCmdName)] = func() (cli.Command, error) {
				return sub, nil
			}
		}

	}
	return cmds
}

type command struct {
	h       func() string
	r       func(args []string) int
	summary string
}

func (c *command) Run(args []string) int {
	return c.r(args)
}
func (c *command) Help() string {
	return c.h()
}
func (c *command) Synopsis() string {
	return c.summary
}

type apiCommand struct {
	l       *lytics.Client
	f       *flag.FlagSet
	ui      cli.Ui
	aid     int
	format  string
	apiKey  string
	dataKey string
	args    []string
	cols    []string
}

func (c *apiCommand) init(args []string, help func() string) {
	c.args = args
	c.f.Usage = func() { c.ui.Output(help()) }

	format := os.Getenv("LYTICSFORMAT")
	if format == "" {
		format = "table"
	}
	c.f.IntVar(&c.aid, "aid", 0, "Account aid")
	c.f.StringVar(&c.format, "format", format, "Output format Reads LYTICSFORMAT envvar as default")
	c.f.StringVar(&c.apiKey, "key", os.Getenv("LIOKEY"), "Api Key, Reads LIOKEY envvar as default")
	c.f.StringVar(&c.dataKey, "datakey", os.Getenv("LIODATAKEY"), "Data Key, Reads LIODATAKEY envvar as default")

	if err := c.f.Parse(c.args); err != nil {
		c.ui.Error(fmt.Sprintf("Could not parse args %v", err))
		os.Exit(1)
	}

	// create lytics client with auth info
	c.l = lytics.NewLytics(c.apiKey, c.dataKey, nil)
}
func (c *apiCommand) writeTable(item interface{}) {
	table := termtables.CreateTable()

	cw := datasource.NewContextWrapper(item)

	row := make([]interface{}, len(c.cols))
	for i, col := range c.cols {
		table.AddHeaders(col)
		val, _ := cw.Get(col)
		if val != nil {
			row[i] = val.Value()
		}
	}

	table.AddRow(row...)

	fmt.Println(table.Render())
}
func (c *apiCommand) writeTableList(items []interface{}) {
	table := termtables.CreateTable()
	for _, col := range c.cols {
		table.AddHeaders(col)
	}

	for _, item := range items {
		cw := datasource.NewContextWrapper(item)
		row := make([]interface{}, len(c.cols))
		for i, col := range c.cols {
			val, _ := cw.Get(col)
			if val != nil {
				row[i] = val.Value()
			}
		}
		table.AddRow(row...)
	}

	fmt.Println(table.Render())
}
func (c *apiCommand) writeSingle(item interface{}) {
	if c.format == "table" {
		c.writeTable(item)
		return
	}
	jsonOut, err := json.MarshalIndent(item, "", "	")
	if err != nil {
		c.ui.Error(fmt.Sprintf("Failed to marshal json? %v", err))
	}
	c.ui.Output(string(jsonOut))
}
func (c *apiCommand) writeList(items []interface{}) {
	if c.format == "table" {
		c.writeTableList(items)
		return
	}
	jsonOut, err := json.MarshalIndent(items, "", "	")
	if err != nil {
		c.ui.Error(fmt.Sprintf("Failed to marshal json? %v", err))
	}
	c.ui.Output(string(jsonOut))
}

func (c *apiCommand) exitIfErr(err error, msg string) {
	if err != nil {
		c.ui.Error(fmt.Sprintf("%v: %s\n", err, msg))
		os.Exit(1)
	}
}
