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

func (h *handler) recentKifu(ctx context.Context, reqCtx *requestcontext.Context, in *kifupb.RecentKifuRequest) (*kifupb.RecentKifuResponse, error) {
	kifus, err := h.service.RecentKifu(ctx, reqCtx.UserId, in.Limit)
	if err != nil {
		return nil, err
	}

	var ret []*kifupb.RecentKifuResponse_Kifu
	for _, k := range kifus {
		ret = append(ret, &kifupb.RecentKifuResponse_Kifu{
			UserId:        k.UserId,
			KifuId:        k.KifuId,
			StartTs:       k.Start.Unix(),
			Handicap:      k.Handicap,
			GameName:      k.GameName,
			FirstPlayers:  k.FirstPlayers,
			SecondPlayers: k.SecondPlayers,
			Note:          k.Note,
		})
	}

	return &kifupb.RecentKifuResponse{Kifus: ret}, nil
}

func (h *handler) Invoke(ctx context.Context, in *kifupb.RecentKifuRequest) (*kifupb.RecentKifuResponse, error) {
	lc, ok := lambdacontext.FromContext(ctx)
	if !ok {
		return nil, errors.New("no lambdacontext")
	}
	reqCtx := requestcontext.Decode(lc.ClientContext.Custom)

	kifus, err := h.service.RecentKifu(ctx, reqCtx.UserId, in.Limit)
	if err != nil {
		return nil, err
	}

	var ret []*kifupb.RecentKifuResponse_Kifu
	for _, k := range kifus {
		ret = append(ret, &kifupb.RecentKifuResponse_Kifu{
			UserId:        k.UserId,
			KifuId:        k.KifuId,
			StartTs:       k.Start.Unix(),
			Handicap:      k.Handicap,
			GameName:      k.GameName,
			FirstPlayers:  k.FirstPlayers,
			SecondPlayers: k.SecondPlayers,
			Note:          k.Note,
		})
	}

	return &kifupb.RecentKifuResponse{Kifus: ret}, nil
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

	lambda.StartWithContext(ctx, h.Invoke)
}
