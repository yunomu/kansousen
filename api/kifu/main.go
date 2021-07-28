package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"google.golang.org/protobuf/encoding/protojson"

	"go.uber.org/zap"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"

	"github.com/aws/aws-lambda-go/events"
	runtime "github.com/aws/aws-lambda-go/lambda"

	apipb "github.com/yunomu/kansousen/proto/api"

	"github.com/yunomu/kansousen/proto/lambdakifu"
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
	kifuFuncArn  string
	lambdaClient *lambda.Lambda

	marshaler   *protojson.MarshalOptions
	unmarshaler *protojson.UnmarshalOptions
}

func convRequest(userId string, req *apipb.KifuRequest) (*lambdakifu.Input, error) {
	in := &lambdakifu.Input{}

	switch t := req.KifuRequestSelect.(type) {
	case *apipb.KifuRequest_RequestRecentKifu:
		r := t.RequestRecentKifu
		in.Select = &lambdakifu.Input_RecentKifuInput{
			RecentKifuInput: &lambdakifu.RecentKifuInput{
				UserId: userId,
				Limit:  r.Limit,
			},
		}
	case *apipb.KifuRequest_RequestPostKifu:
		r := t.RequestPostKifu

		encoding, ok := lambdakifu.PostKifuInput_Encoding_value[r.Encoding]
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "unknown encoding")
		}

		format, ok := lambdakifu.PostKifuInput_Format_value[r.Format]
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "unknown format")
		}

		in.Select = &lambdakifu.Input_PostKifuInput{
			PostKifuInput: &lambdakifu.PostKifuInput{
				UserId:   userId,
				Encoding: lambdakifu.PostKifuInput_Encoding(encoding),
				Format:   lambdakifu.PostKifuInput_Format(format),
				Payload:  r.Payload,
			},
		}
	case *apipb.KifuRequest_RequestDeleteKifu:
		r := t.RequestDeleteKifu
		in.Select = &lambdakifu.Input_DeleteKifuInput{
			DeleteKifuInput: &lambdakifu.DeleteKifuInput{
				UserId: userId,
				KifuId: r.KifuId,
			},
		}
	case *apipb.KifuRequest_RequestGetKifu:
		r := t.RequestGetKifu
		in.Select = &lambdakifu.Input_GetKifuInput{
			GetKifuInput: &lambdakifu.GetKifuInput{
				UserId: userId,
				KifuId: r.KifuId,
			},
		}
	case *apipb.KifuRequest_RequestGetSamePositions:
		r := t.RequestGetSamePositions
		in.Select = &lambdakifu.Input_GetSamePositionsInput{
			GetSamePositionsInput: &lambdakifu.GetSamePositionsInput{
				UserId:         userId,
				Position:       r.Position,
				Steps:          r.Steps,
				ExcludeKifuIds: r.ExcludeKifuIds,
			},
		}
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown request")
	}

	return in, nil
}

func convResponse(out *lambdakifu.Output) (*apipb.KifuResponse, error) {
	res := &apipb.KifuResponse{}

	switch t := out.Select.(type) {
	case *lambdakifu.Output_GetKifuOutput:
		o := t.GetKifuOutput
		var _ = o
	case *lambdakifu.Output_RecentKifuOutput:
		o := t.RecentKifuOutput
		var kifus []*apipb.RecentKifuResponse_Kifu
		for _, kifu := range o.Kifus {
			kifus = append(kifus, &apipb.RecentKifuResponse_Kifu{
				UserId:  kifu.UserId,
				KifuId:  kifu.KifuId,
				StartTs: kifu.StartTs,

				Handicap:     kifu.Handicap,
				GameName:     kifu.GameName,
				FirstPlayer:  strings.Join(kifu.FirstPlayers, ", "),
				SecondPlayer: strings.Join(kifu.SecondPlayers, ", "),
				Note:         kifu.Note,
			})
		}

		res.KifuResponseSelect = &apipb.KifuResponse_ResponseRecentKifu{
			ResponseRecentKifu: &apipb.RecentKifuResponse{
				Kifus: kifus,
			},
		}
	case *lambdakifu.Output_PostKifuOutput:
		o := t.PostKifuOutput
		res.KifuResponseSelect = &apipb.KifuResponse_ResponsePostKifu{
			ResponsePostKifu: &apipb.PostKifuResponse{
				KifuId: o.KifuId,
			},
		}
	case *lambdakifu.Output_DeleteKifuOutput:
		res.KifuResponseSelect = &apipb.KifuResponse_ResponseDeleteKifu{
			ResponseDeleteKifu: &apipb.DeleteKifuResponse{},
		}
	case *lambdakifu.Output_GetSamePositionsOutput:
	default:
		return nil, status.Errorf(codes.Unimplemented, "unknown operation")
	}

	return res, nil
}

type request events.APIGatewayProxyRequest
type response events.APIGatewayProxyResponse

func getUserId(ctx *events.APIGatewayProxyRequestContext) string {
	claimsVal, ok := ctx.Authorizer["claims"]
	if !ok {
		return ""
	}

	claims, ok := claimsVal.(map[string]interface{})
	if !ok {
		return ""
	}

	userIdVal, ok := claims["sub"]
	if !ok {
		return ""
	}

	userId, ok := userIdVal.(string)
	if !ok {
		return ""
	}

	return userId
}

func (s *server) kifu(ctx context.Context, r *request) (*response, error) {
	headers := map[string]string{
		"Access-Control-Allow-Origin": "*",
	}

	userId := getUserId(&r.RequestContext)
	if userId == "" {
		return nil, errors.New("sub is not found in claims")
	}

	req := &apipb.KifuRequest{}
	if err := s.unmarshaler.Unmarshal([]byte(r.Body), req); err != nil {
		return nil, err
	}

	in, err := convRequest(userId, req)
	if err != nil {
		return nil, err
	}

	bs, err := s.marshaler.Marshal(in)
	if err != nil {
		return nil, err
	}

	o, err := s.lambdaClient.InvokeWithContext(ctx, &lambda.InvokeInput{
		FunctionName:   aws.String(s.kifuFuncArn),
		InvocationType: aws.String(lambda.InvocationTypeRequestResponse),
		Payload:        bs,
	})
	if err != nil {
		return nil, err
	}

	if o.FunctionError != nil {
		d := json.NewDecoder(strings.NewReader(aws.StringValue(o.FunctionError)))
		v := map[string]string{}
		if err := d.Decode(v); err != nil {
			return nil, err
		}
		// TODO: v["errorType"]判別
		return nil, err
	}

	out := &lambdakifu.Output{}
	if err := s.unmarshaler.Unmarshal(o.Payload, out); err != nil {
		return nil, err
	}

	res, err := convResponse(out)
	if err != nil {
		return nil, err
	}

	outBs, err := s.marshaler.Marshal(res)
	if err != nil {
		return nil, err
	}

	headers["Content-Type"] = "application/json"
	return &response{
		StatusCode: 200,
		Headers:    headers,
		Body:       string(outBs),
	}, nil
}

func (s *server) handler(ctx context.Context, req *request) (*response, error) {
	header := map[string]string{
		"Access-Control-Allow-Origin": "*",
	}

	switch req.HTTPMethod {
	case "POST":
		switch req.Path {
		case "/v1/kifu":
			return s.kifu(ctx, req)
		default:
			return &response{
				StatusCode: 404,
				Headers:    header,
			}, nil
		}
	default:
		return &response{
			StatusCode: 405,
			Headers:    header,
		}, nil
	}
}

func main() {
	ctx := context.Background()

	kifuFuncArn := os.Getenv("KIFU_FUNC_ARN")
	if kifuFuncArn == "" {
		zap.L().Fatal("env KIFU_FUNC_ARN is not found")
	}

	region := os.Getenv("REGION")

	session := session.New()
	lambdaClient := lambda.New(session, aws.NewConfig().WithRegion(region))

	s := &server{
		kifuFuncArn:  kifuFuncArn,
		lambdaClient: lambdaClient,
		marshaler:    &protojson.MarshalOptions{},
		unmarshaler: &protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	}

	runtime.StartWithContext(ctx, s.handler)
}
