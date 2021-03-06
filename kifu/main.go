package main

import (
	"context"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/golang/protobuf/proto"

	"github.com/google/uuid"

	"go.uber.org/zap"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	awsdynamodb "github.com/aws/aws-sdk-go/service/dynamodb"

	"github.com/yunomu/kif"

	"github.com/yunomu/kansousen/lib/awsx"
	"github.com/yunomu/kansousen/lib/db"
	libdynamodb "github.com/yunomu/kansousen/lib/dynamodb"
	libkifu "github.com/yunomu/kansousen/lib/kifu"
	"github.com/yunomu/kansousen/lib/lambda"
	"github.com/yunomu/kansousen/lib/pbconv"
	apipb "github.com/yunomu/kansousen/proto/api"
	documentpb "github.com/yunomu/kansousen/proto/document"
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
	table db.DB
}

var _ lambda.Server = (*server)(nil)

func (s *server) recentKifu(ctx context.Context, userId string, req *apipb.RecentKifuRequest) (*apipb.KifuResponse, error) {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "time.LoadLocation: %v", err)
	}

	kifus, err := s.table.GetRecentKifu(ctx, userId, int(req.Limit))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "GetRecentKifu: %v", err)
	}

	var ret []*apipb.RecentKifuResponse_Kifu
	for _, kifu := range kifus {
		var firstPlayers, secondPlayers []string
		for _, player := range kifu.Players {
			switch player.Order {
			case documentpb.Player_BLACK:
				firstPlayers = append(firstPlayers, player.GetName())
			case documentpb.Player_WHITE:
				secondPlayers = append(secondPlayers, player.GetName())
			}
		}

		ret = append(ret, &apipb.RecentKifuResponse_Kifu{
			UserId:  kifu.GetUserId(),
			KifuId:  kifu.GetKifuId(),
			StartTs: pbconv.DateTimeToTS(kifu.GetStart(), loc),

			Handicap:     kifu.GetHandicap().String(),
			GameName:     kifu.GetGameName(),
			FirstPlayer:  strings.Join(firstPlayers, ", "),
			SecondPlayer: strings.Join(secondPlayers, ", "),
			Note:         kifu.GetNote(),
		})
	}

	return &apipb.KifuResponse{
		KifuResponseSelect: &apipb.KifuResponse_ResponseRecentKifu{
			ResponseRecentKifu: &apipb.RecentKifuResponse{
				Kifus: ret,
			},
		},
	}, nil
}

func (s *server) postKifu(ctx context.Context, userId string, req *apipb.PostKifuRequest) (*apipb.KifuResponse, error) {
	var parseOptions []kif.ParseOption
	switch req.Encoding {
	case "UTF-8":
		parseOptions = append(parseOptions, kif.ParseEncodingUTF8())
	case "Shift_JIS":
		parseOptions = append(parseOptions, kif.ParseEncodingSJIS())
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unavailable encoding: `%v`", req.Encoding)
	}

	switch req.Format {
	case "KIF":
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unavailable format: `%v`", req.Format)
	}

	kifuUUID, err := uuid.NewRandom()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Generate UUID: %v", err)
	}

	parser := libkifu.NewParser(kif.NewParser(parseOptions...))

	kifu, steps, err := parser.Parse(strings.NewReader(req.Payload), userId, kifuUUID.String())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "ParseError: %v", err)
	}

	if err := s.table.PutKifu(ctx, kifu, steps); err != nil {
		return nil, status.Errorf(codes.Internal, "PutKifu: %v", err)
	}

	var dups []*apipb.PostKifuResponse_Kifu

	sigs, err := s.table.DuplicateKifu(ctx, kifu.Sfen)
	if err != nil {
		zap.L().Warn("DuplicateKifu",
			zap.Error(err),
			zap.String("sfen", kifu.Sfen),
			zap.String("kifuId", kifu.KifuId),
		)
	}
	for _, sig := range sigs {
		dups = append(dups, &apipb.PostKifuResponse_Kifu{
			KifuId: sig.KifuId,
		})
	}

	return &apipb.KifuResponse{
		KifuResponseSelect: &apipb.KifuResponse_ResponsePostKifu{
			ResponsePostKifu: &apipb.PostKifuResponse{
				KifuId:     kifuUUID.String(),
				Duplicated: dups,
			},
		},
	}, nil
}

func (s *server) deleteKifu(ctx context.Context, userId string, req *apipb.DeleteKifuRequest) (*apipb.KifuResponse, error) {
	if err := s.table.DeleteKifu(ctx, userId, req.KifuId); err != nil {
		return nil, status.Errorf(codes.Internal, "DeleteKifu: %v", err)
	}

	return &apipb.KifuResponse{
		KifuResponseSelect: &apipb.KifuResponse_ResponseDeleteKifu{
			ResponseDeleteKifu: &apipb.DeleteKifuResponse{},
		},
	}, nil
}

func (s *server) getKifu(ctx context.Context, userId string, req *apipb.GetKifuRequest) (*apipb.KifuResponse, error) {
	kifu, steps, err := s.table.GetKifuAndSteps(ctx, userId, req.GetKifuId())
	if err != nil {
		return nil, err
	}

	var resSteps []*apipb.GetKifuResponse_Step
	for _, step := range steps {
		var resStep *apipb.GetKifuResponse_Step

		resStep = &apipb.GetKifuResponse_Step{
			Seq:          step.GetSeq(),
			Position:     step.GetPosition(),
			Promoted:     step.GetPromote(),
			Captured:     apipb.Piece_Id(step.GetCaptured()),
			TimestampSec: step.GetTimestampSec(),
			ThinkingSec:  step.GetThinkingSec(),
			Notes:        step.Notes,

			FinishedStatus: apipb.FinishedStatus_Id(step.GetFinishedStatus()),
		}

		if dst := step.GetDst(); dst != nil {
			resStep.Dst = &apipb.Pos{
				X: dst.GetX(),
				Y: dst.GetY(),
			}
		}
		resStep.Piece = apipb.Piece_Id(step.GetPiece())
		if src := step.GetSrc(); src != nil {
			resStep.Src = &apipb.Pos{
				X: src.GetX(),
				Y: src.GetY(),
			}
		}

		resSteps = append(resSteps, resStep)
	}

	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "time.LoadLocation: %v", err)
	}

	var firstPlayers, secondPlayers []*apipb.GetKifuResponse_Player
	for _, player := range kifu.Players {
		switch player.Order {
		case documentpb.Player_BLACK:
			firstPlayers = append(firstPlayers, &apipb.GetKifuResponse_Player{
				Name: player.GetName(),
				Note: player.GetNote(),
			})
		case documentpb.Player_WHITE:
			secondPlayers = append(secondPlayers, &apipb.GetKifuResponse_Player{
				Name: player.GetName(),
				Note: player.GetNote(),
			})
		}
	}

	var otherFields []*apipb.Value
	for k, v := range kifu.OtherFields {
		otherFields = append(otherFields, &apipb.Value{
			Name:  k,
			Value: v,
		})
	}

	return &apipb.KifuResponse{
		KifuResponseSelect: &apipb.KifuResponse_ResponseGetKifu{
			ResponseGetKifu: &apipb.GetKifuResponse{
				UserId: kifu.GetUserId(),
				KifuId: kifu.GetKifuId(),

				StartTs:       pbconv.DateTimeToTS(kifu.GetStart(), loc),
				EndTs:         pbconv.DateTimeToTS(kifu.GetEnd(), loc),
				Handicap:      kifu.GetHandicap().String(),
				GameName:      kifu.GetGameName(),
				FirstPlayers:  firstPlayers,
				SecondPlayers: secondPlayers,
				OtherFields:   otherFields,
				Sfen:          kifu.GetSfen(),
				CreatedTs:     kifu.GetCreatedTs(),
				Steps:         resSteps,
				Note:          kifu.GetNote(),
			},
		},
	}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	} else {
		return b
	}
}

func (s *server) getSamePositions(ctx context.Context, userId string, req *apipb.GetSamePositionsRequest) (*apipb.KifuResponse, error) {
	pss, err := s.table.GetSamePositions(ctx,
		[]string{userId},
		req.Position,
		db.GetSamePositionsSetNumStep(req.GetSteps()),
		db.GetSamePositionsAddExcludeKifuIds(req.GetExcludeKifuIds()),
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "GetSamePositions: %v", err)
	}

	var kifus []*apipb.GetSamePositionsResponse_Kifu
	for _, ps := range pss {
		var steps []*apipb.GetSamePositionsResponse_Step
		for _, step := range ps.Steps {
			var src, dst *apipb.Pos
			if step.GetDst() != nil {
				dst = &apipb.Pos{
					X: step.Dst.X,
					Y: step.Dst.Y,
				}
			}
			if step.GetSrc() != nil {
				src = &apipb.Pos{
					X: step.Src.X,
					Y: step.Src.Y,
				}
			}
			steps = append(steps, &apipb.GetSamePositionsResponse_Step{
				Seq:            step.GetSeq(),
				Dst:            dst,
				Src:            src,
				Piece:          apipb.Piece_Id(step.GetPiece()),
				Promoted:       step.Promote,
				FinishedStatus: apipb.FinishedStatus_Id(step.GetFinishedStatus()),
			})
		}

		kifus = append(kifus, &apipb.GetSamePositionsResponse_Kifu{
			UserId: ps.Position.GetUserId(),
			KifuId: ps.Position.GetKifuId(),
			Seq:    ps.Position.GetSeq(),
			Steps:  steps,
		})
	}

	return &apipb.KifuResponse{
		KifuResponseSelect: &apipb.KifuResponse_ResponseGetSamePositions{
			ResponseGetSamePositions: &apipb.GetSamePositionsResponse{
				Position: req.Position,
				Kifus:    kifus,
			},
		},
	}, nil
}

func (s *server) Serve(ctx context.Context, m proto.Message) (proto.Message, error) {
	userId := lambda.GetUserId(ctx)
	if userId == "" {
		return nil, status.Errorf(codes.Unauthenticated, "Invalid authentication data")
	}

	req := m.(*apipb.KifuRequest)
	switch t := req.KifuRequestSelect.(type) {
	case *apipb.KifuRequest_RequestRecentKifu:
		return s.recentKifu(ctx, userId, t.RequestRecentKifu)
	case *apipb.KifuRequest_RequestPostKifu:
		return s.postKifu(ctx, userId, t.RequestPostKifu)
	case *apipb.KifuRequest_RequestDeleteKifu:
		return s.deleteKifu(ctx, userId, t.RequestDeleteKifu)
	case *apipb.KifuRequest_RequestGetKifu:
		return s.getKifu(ctx, userId, t.RequestGetKifu)
	case *apipb.KifuRequest_RequestGetSamePositions:
		return s.getSamePositions(ctx, userId, t.RequestGetSamePositions)
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown request")
	}
}

func main() {
	ctx := context.Background()

	tableName := os.Getenv("TABLE_NAME")
	if tableName == "" {
		zap.L().Fatal("env TABLE_NAME is not found")
	}

	region := "ap-northeast-1"

	kv, err := awsx.GetSecrets(ctx, region, os.Getenv("SECRET_NAME"))
	if err != nil {
		zap.L().Fatal("awsx.GetSecrets", zap.Error(err))
	}

	cred := credentials.NewStaticCredentials(
		kv["AWS_ACCESS_KEY_ID"],
		kv["AWS_SECRET_ACCESS_KEY"],
		"",
	)
	session := session.New(aws.NewConfig().WithCredentials(cred))

	dynamodb := awsdynamodb.New(session, aws.NewConfig().WithRegion(region))

	table := libdynamodb.NewDynamoDBTable(dynamodb, tableName)
	if err := table.Init(ctx); err != nil {
		zap.L().Fatal("DynamoDBTable.Init", zap.Error(err), zap.String("tableName", tableName))
	}

	s := &server{
		table: db.NewDynamoDB(table),
	}

	h := lambda.NewProtobufHandler((*apipb.KifuRequest)(nil), s)

	h.Start(ctx)
}
