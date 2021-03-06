package db

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/google/subcommands"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	awsdynamodb "github.com/aws/aws-sdk-go/service/dynamodb"

	"github.com/yunomu/kansousen/lib/db"
	"github.com/yunomu/kansousen/lib/dynamodb"

	"github.com/yunomu/kansousen/cmd/db/deletekifu"
	"github.com/yunomu/kansousen/cmd/db/getkifu"
	"github.com/yunomu/kansousen/cmd/db/getsteps"
	"github.com/yunomu/kansousen/cmd/db/listkifu"
	"github.com/yunomu/kansousen/cmd/db/putkifu"
	"github.com/yunomu/kansousen/cmd/db/recentkifu"
	"github.com/yunomu/kansousen/cmd/db/samepos"
)

type Command struct {
	endpoint *string
	region   *string
	table    *string
	log      *bool

	commander *subcommands.Commander
}

func NewCommand() *Command {
	return &Command{}
}

func (c *Command) Name() string     { return "db" }
func (c *Command) Synopsis() string { return "db test" }
func (c *Command) Usage() string {
	return `
`
}

func (c *Command) SetFlags(f *flag.FlagSet) {
	f.SetOutput(os.Stderr)

	c.endpoint = f.String("endpoint", "", "Endpoint of DynamoDB")
	c.region = f.String("region", "ap-northeast-1", "Endpoint of DynamoDB")
	c.table = f.String("table", "kansousen", "Table name")
	c.log = f.Bool("log", false, "output log")

	commander := subcommands.NewCommander(f, "")
	commander.Register(commander.FlagsCommand(), "help")
	commander.Register(commander.CommandsCommand(), "help")
	commander.Register(commander.HelpCommand(), "help")

	commander.Register(putkifu.NewCommand(), "kifu")
	commander.Register(getkifu.NewCommand(), "kifu")
	commander.Register(listkifu.NewCommand(), "kifu")
	commander.Register(deletekifu.NewCommand(), "kifu")
	commander.Register(recentkifu.NewCommand(), "kifu")

	commander.Register(getsteps.NewCommand(), "step")

	commander.Register(samepos.NewCommand(), "pos")

	c.commander = commander
}

type logger struct{}

func (l *logger) Info(function string, message string) {
	log.Println(function, message)
}

func (c *Command) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	return c.commander.Execute(ctx, func() db.DB {
		config := aws.NewConfig().WithRegion(*c.region)

		if *c.endpoint != "" {
			config.WithEndpoint(*c.endpoint)
		}

		var opts []dynamodb.DynamoDBTableOption
		if *c.log {
			opts = append(opts, dynamodb.SetLogger(&logger{}))
		}

		tab := dynamodb.NewDynamoDBTable(
			awsdynamodb.New(session.New(), config),
			*c.table,
			opts...,
		)

		return db.NewDynamoDB(tab)
	})
}
