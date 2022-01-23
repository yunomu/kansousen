package main

import (
	"context"
	"os"

	"go.uber.org/zap"

	lambdaclient "github.com/aws/aws-lambda-go/lambda"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"

	"github.com/yunomu/kansousen/lib/lambda/lambdagateway"
	"github.com/yunomu/kansousen/lib/lambda/lambdarpc"
)

var logger *zap.Logger

func init() {
	l, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	logger = l
}

type apiLogger struct{}

func (*apiLogger) Error(msg string, err error) {
	logger.Error(msg, zap.Error(err))
}

func main() {
	ctx := context.Background()

	region := os.Getenv("REGION")
	if region == "" {
		zap.L().Fatal("Getenv", zap.String("key", "REGION"))
	}
	kifuFuncArn := os.Getenv("KIFU_FUNCTION")
	if kifuFuncArn == "" {
		zap.L().Fatal("Getenv", zap.String("key", "KIFU_FUNCTION"))
	}
	basePath := os.Getenv("BASE_PATH")

	session := session.New()
	lambdaClient := lambda.New(session, aws.NewConfig().WithRegion(region))

	gw := lambdagateway.NewLambdaGateway(lambdaClient,
		lambdagateway.WithAPIRequestID(lambdarpc.ApiRequestIdField),
		lambdagateway.WithClaimSubID(lambdarpc.UserIdField),
		lambdagateway.AddFunction("/v1/post-kifu", "POST", kifuFuncArn, "PostKifu"),
		lambdagateway.AddFunction("/v1/get-kifu", "POST", kifuFuncArn, "GetKifu"),
		lambdagateway.AddFunction("/v1/delete-kifu", "POST", kifuFuncArn, "DeleteKifu"),
		lambdagateway.AddFunction("/v1/recent-kifu", "POST", kifuFuncArn, "RecentKifu"),
		lambdagateway.AddFunction("/v1/same-positions", "POST", kifuFuncArn, "GetSamePositions"),
		lambdagateway.SetBasePath(basePath),
		lambdagateway.SetLogger(&apiLogger{}),
		lambdagateway.SetFunctionErrorHandler(func(e *lambdagateway.LambdaError) error {
			switch e.ErrorType {
			case "InvalidArgumentError":
				return lambdagateway.ClientError(400, e.ErrorMessage)
			default:
				zap.L().Error("lambda.Invoke", zap.Any("error", e))
				return lambdagateway.ServerError()
			}
		}),
	)

	lambdaclient.StartWithContext(ctx, gw.Serve)
}
