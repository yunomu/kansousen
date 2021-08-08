package main

import (
	"context"
	"os"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"go.uber.org/zap"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"

	apipb "github.com/yunomu/kansousen/proto/kifu"

	"github.com/yunomu/kansousen/lib/config"
	"github.com/yunomu/kansousen/lib/lambda/apihandler"
	"github.com/yunomu/kansousen/lib/lambda/lambdarpc"
	"github.com/yunomu/kansousen/lib/lambda/requestcontext"
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

func (s *server) kifu(ctx context.Context, reqCtx *requestcontext.Context, r *apihandler.Request) (proto.Message, apihandler.Error) {
	req := &apipb.KifuRequest{}
	if err := s.unmarshaler.Unmarshal([]byte(r.Body), req); err != nil {
		return nil, apihandler.ClientError(400, err.Error())
	}

	res := &apipb.KifuResponse{}
	if err := s.lambdaClient.Invoke(ctx, reqCtx, req, res); err != nil {
		switch err.(type) {
		case *lambdarpc.LambdaError:
			e := err.(*lambdarpc.LambdaError)
			// TODO: errorType client error
			zap.L().Error("LambdaInvoke",
				zap.String("errorType", e.ErrorType),
				zap.String("errorMessage", e.ErrorMessage),
			)
			return nil, apihandler.ServerError()
		default:
			zap.L().Error("LambdaInvoke", zap.Error(err))
			return nil, apihandler.ServerError()
		}
	}

	return res, nil
}

func (s *server) kifuRecent(ctx context.Context, reqCtx *requestcontext.Context, r *apihandler.Request) (proto.Message, apihandler.Error) {
	return nil, nil
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

	s := &server{
		lambdaClient:     lambdarpc.NewClient(lambdaClient, kifuFuncArn),
		kifuRecentLambda: lambdarpc.NewClient(lambdaClient, kifuRecentFunc),
		unmarshaler: &protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	}

	h := apihandler.NewHandler(
		apihandler.AddHandler("/v1/kifu", "POST", s.kifu),
		apihandler.SetLogger(&apiLogger{}),
	)

	h.Start(ctx)
}
