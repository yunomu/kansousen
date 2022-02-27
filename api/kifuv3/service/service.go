package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yunomu/kif"

	"github.com/yunomu/kansousen/lib/db"
	libkifu "github.com/yunomu/kansousen/lib/kifu"
	"github.com/yunomu/kansousen/lib/lambda/lambdarpc"
	documentpb "github.com/yunomu/kansousen/proto/document"
	kifupb "github.com/yunomu/kansousen/proto/kifu"
)

type KifuServiceError interface {
	error
	Type() string
}

type Service struct {
	table db.DB
}

func NewService(table db.DB) *Service {
	return &Service{
		table: table,
	}
}

type searchOptions struct {
	limit int
	steps int
}

type SearchOption func(*searchOptions)

func SetSearchLimit(limit int) SearchOption {
	return func(o *searchOptions) {
		o.limit = limit
	}
}

func SetSearchSteps(steps int) SearchOption {
	return func(o *searchOptions) {
		o.steps = steps
	}
}

func (s *Service) Search(
	ctx context.Context,
	userId string,
	opts ...SearchOption,
) ([]*documentpb.Kifu, error) {
	o := &searchOptions{
		limit: 10,
		steps: 0,
	}
	for _, f := range opts {
		f(o)
	}

	kifus, err := s.table.GetRecentKifu(ctx, userId, o.limit)
	if err != nil {
		return nil, err
	}

	return kifus, nil
}

type InvalidArgumentError struct {
	typ string
	msg string
}

var _ KifuServiceError = (*InvalidArgumentError)(nil)

func (e *InvalidArgumentError) Error() string {
	return e.msg
}

func (e *InvalidArgumentError) Type() string {
	return e.typ
}

type Format string

const (
	Format_KIF Format = "KIF"
)

var ErrUnknownFormat = errors.New("Unknown format")

type Encoding string

const (
	Encoding_UTF8     Encoding = "UTF-8"
	Encoding_ShiftJIS Encoding = "Shift_JIS"
)

var ErrUnknownEncoding = errors.New("Unknown encoding")

func (s *Service) PostKifu(
	ctx context.Context,
	userId string,
	payload io.Reader,
	format Format,
	encoding Encoding,
	loc *time.Location,
) (string, int64, error) {
	var parseOptions []kif.ParseOption
	switch encoding {
	case Encoding_UTF8:
		parseOptions = append(parseOptions, kif.ParseEncodingUTF8())
	case Encoding_ShiftJIS:
		parseOptions = append(parseOptions, kif.ParseEncodingSJIS())
	default:
		return "", 0, ErrUnknownEncoding
	}

	switch format {
	case Format_KIF:
	default:
		return "", 0, ErrUnknownFormat
	}

	kifuUUID, err := uuid.NewRandom()
	if err != nil {
		return "", 0, err
	}

	parser := libkifu.NewParser(kif.NewParser(parseOptions...), loc)

	kifu, steps, err := parser.Parse(payload, userId, kifuUUID.String())
	if err != nil {
		return "", 0, err
	}

	version, err := s.table.PutKifu(ctx, kifu, steps, 0)
	if err != nil {
		return "", 0, err
	}

	return kifuUUID.String(), version, nil
}

func (s *Service) DeleteKifu(
	ctx context.Context,
	kifuId string,
	version int64,
) error {
	return s.table.DeleteKifu(ctx, kifuId, version)
}

func (s *Service) GetKifu(
	ctx context.Context,
	kifuId string,
) (*documentpb.Kifu, []*documentpb.Step, int64, error) {
	kifu, steps, version, err := s.table.GetKifuAndSteps(ctx, kifuId)
	if err != nil {
		return nil, nil, 0, err
	}

	return kifu, steps, version, err
}

func (s *Service) GetSamePositions(ctx context.Context, req *kifupb.GetSamePositionsRequest) (*kifupb.GetSamePositionsResponse, error) {
	userId := lambdarpc.GetUserId(ctx)
	if userId == "" {
		return nil, &lambdarpc.ClientError{
			Message: "user-id is not found",
		}
	}

	pss, err := s.table.GetSamePositions(ctx,
		[]string{userId},
		req.GetPosition(),
		db.GetSamePositionsSetNumStep(req.GetSteps()),
		db.GetSamePositionsAddExcludeKifuIds(req.GetExcludeKifuIds()),
	)
	if err != nil {
		return nil, &lambdarpc.InternalError{
			Message: "GetSamePositions",
			Err:     err,
		}
	}

	var kifus []*kifupb.GetSamePositionsResponse_Kifu
	for _, ps := range pss {
		var steps []*kifupb.GetSamePositionsResponse_Step
		for _, step := range ps.Steps {
			var src, dst *kifupb.Pos
			if step.GetDst() != nil {
				dst = &kifupb.Pos{
					X: step.Dst.X,
					Y: step.Dst.Y,
				}
			}
			if step.GetSrc() != nil {
				src = &kifupb.Pos{
					X: step.Src.X,
					Y: step.Src.Y,
				}
			}
			steps = append(steps, &kifupb.GetSamePositionsResponse_Step{
				Seq:            step.GetSeq(),
				Dst:            dst,
				Src:            src,
				Piece:          kifupb.Piece_Id(step.GetPiece()),
				Promoted:       step.Promote,
				FinishedStatus: kifupb.FinishedStatus_Id(step.GetFinishedStatus()),
			})
		}

		kifus = append(kifus, &kifupb.GetSamePositionsResponse_Kifu{
			UserId: ps.UserId,
			KifuId: ps.KifuId,
			Steps:  steps,
		})
	}

	return &kifupb.GetSamePositionsResponse{
		Position: req.GetPosition(),
		Kifus:    kifus,
	}, nil
}
