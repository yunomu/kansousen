package main

import (
	"context"
	"os"

	"google.golang.org/protobuf/encoding/protojson"

	"go.uber.org/zap"

	lambdaclient "github.com/aws/aws-lambda-go/lambda"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"

	"github.com/yunomu/kansousen/lib/config"
	"github.com/yunomu/kansousen/lib/lambda/lambdagateway"
	"github.com/yunomu/kansousen/lib/lambda/lambdarpc"
)

func init() {
	var logger *zap.Logger
	if os.Getenv("DEV") == "true" {
		l, err := zap.NewDevelopment()
		if err != nil {
			panic(err)
		}
		logger = l
	} else {
		l, err := zap.NewProduction()
		if err != nil {
			panic(err)
		}
		logger = l
	}
	zap.ReplaceGlobals(logger)
}

type server struct {
	lambdaClient     *lambdarpc.Client
	kifuRecentLambda *lambdarpc.Client
	unmarshaler      *protojson.UnmarshalOptions
}

type apiLogger struct{}

func (*apiLogger) Error(msg string, err error) {
	zap.L().Error(msg, zap.Error(err))
}

func main() {
	ctx := context.Background()

	kifuFuncArn := os.Getenv("KIFU_FUNC_ARN")
	if kifuFuncArn == "" {
		zap.L().Fatal("env KIFU_FUNC_ARN is not found")
	}

	region := os.Getenv("REGION")

	configURL := os.Getenv("CONFIG_URL")
	cfg, err := config.Load(configURL)
	if err != nil {
		zap.L().Fatal("Load config error", zap.Error(err), zap.String("configURL", configURL))
	}

	kifuRecentFunc, ok := cfg["RecentKifuFunction"]
	if !ok {
		zap.L().Fatal("RecentKifuFunction not found in config", zap.Any("config", cfg))
	}

	session := session.New()
	lambdaClient := lambda.New(session, aws.NewConfig().WithRegion(region))

	gw := lambdagateway.NewLambdaGateway(lambdaClient,
		lambdagateway.WithAPIRequestID(),
		lambdagateway.WithClaimSubID(),
		lambdagateway.AddFunction("/v1/kifu", "POST", kifuFuncArn),
		lambdagateway.AddFunction("/v1/recent-kifu", "POST", kifuRecentFunc),
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
