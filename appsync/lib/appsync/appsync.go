package appsync

import (
	"context"

	"github.com/aws/aws-lambda-go/lambda"
)

type Handler[Req any, Res any] interface {
	Serve(context.Context, string, []string, Req) (Res, error)
}

func StartSelectionSetHandler[Req any, Res any](h Handler[Req, Res]) {
	lambda.Start(func(ctx context.Context, p *struct {
		Arguments    Req      `json:"arguments"`
		UserId       string   `json:"user"`
		SelectionSet []string `json:"selectionSet"`
	}) (Res, error) {
		return h.Serve(ctx, p.UserId, p.SelectionSet, p.Arguments)
	})
}

type HandlerFunc[Req any, Res any] func(context.Context, string, []string, Req) (Res, error)

var _ Handler[string, string] = (HandlerFunc[string, string])(nil)

func (f HandlerFunc[Req, Res]) Serve(ctx context.Context, userId string, set []string, req Req) (Res, error) {
	return f(ctx, userId, set, req)
}
