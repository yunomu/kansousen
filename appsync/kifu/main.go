package main

import (
	"os"

	"go.uber.org/zap"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"

	"github.com/yunomu/kansousen/appsync/lib/appsync"
	"github.com/yunomu/kansousen/graphql/model"
	"github.com/yunomu/kansousen/lib/kifudb"

	"github.com/yunomu/kansousen/appsync/kifu/handler"
)

var logger *zap.Logger

func init() {
	l, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	logger = l
}

func main() {
	table := os.Getenv("TABLE_NAME")
	region := os.Getenv("REGION")

	sess, err := session.NewSession()
	if err != nil {
		logger.Fatal("NewSession", zap.Error(err))
	}

	var h appsync.Handler[*handler.Request, *model.Kifu] = handler.NewHandler(
		kifudb.NewDynamoDB(
			dynamodb.New(sess, aws.NewConfig().WithRegion(region)),
			table,
		),
	)

	appsync.StartSelectionSetHandler(h)
}
