package main

import (
	"context"
	"os"

	"go.uber.org/zap"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"

	"github.com/yunomu/kansousen/lib/db"
	"github.com/yunomu/kansousen/lib/lambda/lambdarpc"

	"github.com/yunomu/kansousen/lambda/kifu/service"
)

func init() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}

	zap.ReplaceGlobals(logger)
}

func main() {
	ctx := context.Background()

	region := os.Getenv("REGION")
	if region == "" {
		zap.L().Fatal("Getenv", zap.String("key", "REGION"))
	}
	kifuTable := os.Getenv("KIFU_TABLE")
	if region == "" {
		zap.L().Fatal("Getenv", zap.String("key", "KIFU_TABLE"))
	}

	session := session.New()

	zap.L().Info("Start",
		zap.String("region", region),
		zap.String("table_name", kifuTable),
	)

	dynamodb := dynamodb.New(session, aws.NewConfig().WithRegion(region))
	table := db.NewDynamoDB(dynamodb, kifuTable)
	svc := service.NewService(table)

	h := lambdarpc.NewHandler(svc)

	lambda.StartHandlerWithContext(ctx, h)
}
