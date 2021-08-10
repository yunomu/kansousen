package main

import (
	"context"
	"errors"
	"os"

	"go.uber.org/zap"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-lambda-go/lambdacontext"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	awsdynamodb "github.com/aws/aws-sdk-go/service/dynamodb"

	libdb "github.com/yunomu/kansousen/lib/db"
	libdynamodb "github.com/yunomu/kansousen/lib/dynamodb"
	"github.com/yunomu/kansousen/lib/lambda/requestcontext"
	"github.com/yunomu/kansousen/service/kifu"

	kifupb "github.com/yunomu/kansousen/proto/kifu"
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

type handler struct {
	service     *kifu.Service
	unmarshaler *protojson.UnmarshalOptions
	marshaler   *protojson.MarshalOptions
}

func (h *handler) Invoke(ctx context.Context, payload []byte) ([]byte, error) {
	lc, ok := lambdacontext.FromContext(ctx)
	if !ok {
		return nil, errors.New("no lambdacontext")
	}
	reqCtx := requestcontext.FromCustomMap(lc.ClientContext.Custom)

	in := &kifupb.KifuRequest{}
	if err := h.unmarshaler.Unmarshal(payload, in); err != nil {
		return nil, err
	}

	output := &kifupb.KifuResponse{}
	switch t := in.KifuRequestSelect.(type) {
	case *kifupb.KifuRequest_RequestGetKifu:
		out, err := h.getKifu(ctx, reqCtx, t.RequestGetKifu)
		if err != nil {
			return nil, err
		}
		output.KifuResponseSelect = &kifupb.KifuResponse_ResponseGetKifu{
			ResponseGetKifu: out,
		}
	case *kifupb.KifuRequest_RequestPostKifu:
		out, err := h.postKifu(ctx, reqCtx, t.RequestPostKifu)
		if err != nil {
			return nil, err
		}
		output.KifuResponseSelect = &kifupb.KifuResponse_ResponsePostKifu{
			ResponsePostKifu: out,
		}
	case *kifupb.KifuRequest_RequestRecentKifu:
		out, err := h.recentKifu(ctx, reqCtx, t.RequestRecentKifu)
		if err != nil {
			return nil, err
		}
		output.KifuResponseSelect = &kifupb.KifuResponse_ResponseRecentKifu{
			ResponseRecentKifu: out,
		}
	case *kifupb.KifuRequest_RequestDeleteKifu:
		if err := h.deleteKifu(ctx, reqCtx, t.RequestDeleteKifu); err != nil {
			return nil, err
		}
		output.KifuResponseSelect = &kifupb.KifuResponse_ResponseDeleteKifu{}
	}

	return h.marshaler.Marshal(output)
}

func main() {
	ctx := context.Background()

	tableName := os.Getenv("TABLE_NAME")
	if tableName == "" {
		zap.L().Fatal("env TABLE_NAME is not found")
	}

	region := os.Getenv("REGION")

	session := session.New()

	zap.L().Info("Start",
		zap.String("region", region),
		zap.String("table_name", tableName),
	)

	dynamodb := awsdynamodb.New(session, aws.NewConfig().WithRegion(region))

	table := libdynamodb.NewDynamoDBTable(dynamodb, tableName)
	if err := table.Init(ctx); err != nil {
		zap.L().Fatal("DynamoDBTable.Init", zap.Error(err), zap.String("tableName", tableName))
	}

	h := &handler{
		service:   kifu.NewService(libdb.NewDynamoDB(table)),
		marshaler: &protojson.MarshalOptions{},
		unmarshaler: &protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	}

	lambda.StartHandlerWithContext(ctx, h)
}
