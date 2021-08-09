package lambdagateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdacontext"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lambda"

	"github.com/yunomu/kansousen/lib/lambda/requestcontext"
)

const (
	APIRequestIdField = "api_request_id"
)

type Request events.APIGatewayProxyRequest
type Response events.APIGatewayProxyResponse

type LambdaError struct {
	ErrorType    string `json:"errorType"`
	ErrorMessage string `json:"errorMessage"`
}

func (e *LambdaError) Error() string {
	return e.ErrorMessage
}

type Error interface {
	statusCode() int
	errorType() string
	errorMessage() string
	Error() string
}

type serverError struct{}

func ServerError() Error {
	return &serverError{}
}

var _ Error = (*serverError)(nil)

func (*serverError) statusCode() int {
	return 500
}

func (*serverError) errorType() string {
	return "ServerError"
}

func (*serverError) errorMessage() string {
	return "Internal Server Error"
}

func (e *serverError) Error() string {
	return e.errorMessage()
}

type clientError struct {
	code int
	msg  string
}

func ClientError(statusCode int, errorMessage string) Error {
	return &clientError{
		code: statusCode,
		msg:  errorMessage,
	}
}

var _ Error = (*clientError)(nil)

func (c *clientError) statusCode() int {
	return c.code
}

func (*clientError) errorType() string {
	return "ClientError"
}

func (c *clientError) errorMessage() string {
	return c.msg
}

func (e *clientError) Error() string {
	return e.errorMessage()
}

type Logger interface {
	Error(msg string, err error)
}

type defaultLogger struct{}

var _ Logger = (*defaultLogger)(nil)

func (*defaultLogger) Error(msg string, err error) {}

type function string

type Gateway struct {
	lambdaClient         *lambda.Lambda
	functions            map[string]map[string]function
	functionErrorHandler func(*LambdaError) error
	contextModifiers     []func(*lambdacontext.ClientContext, *Request) error
	logger               Logger
}

type GatewayOption func(*Gateway)

func AddFunction(path, method, functionArn string) GatewayOption {
	return func(s *Gateway) {
		p, ok := s.functions[path]
		if !ok {
			p = map[string]function{}
		}

		p[method] = function(functionArn)
		s.functions[path] = p
	}
}

func AddFunctionErrorHandler(h func(*LambdaError) error) GatewayOption {
	return func(s *Gateway) {
		if h == nil {
			h = func(err *LambdaError) error { return err }
		}
		s.functionErrorHandler = h
	}
}

func WithAPIRequestID() GatewayOption {
	return func(s *Gateway) {
		s.contextModifiers = append(s.contextModifiers, func(cc *lambdacontext.ClientContext, req *Request) error {
			if cc == nil {
				return nil
			}

			if cc.Custom == nil {
				cc.Custom = make(map[string]string)
			}

			cc.Custom["api-request-id"] = req.RequestContext.RequestID

			return nil
		})
	}
}

type AuthorizerError struct {
	Message        string                                `json:"message"`
	RequestContext *events.APIGatewayProxyRequestContext `json:"request_context"`
}

func (e *AuthorizerError) Error() string {
	return e.Message
}

func WithClaimSubID() GatewayOption {
	return func(s *Gateway) {
		s.contextModifiers = append(s.contextModifiers, func(cc *lambdacontext.ClientContext, req *Request) error {
			if cc == nil {
				return nil
			}

			if cc.Custom == nil {
				cc.Custom = make(map[string]string)
			}

			reqCtx := &req.RequestContext

			claimsVal, ok := reqCtx.Authorizer["claims"]
			if !ok {
				return &AuthorizerError{
					Message:        "claims not found",
					RequestContext: reqCtx,
				}
			}

			claims, ok := claimsVal.(map[string]interface{})
			if !ok {
				return &AuthorizerError{
					Message:        "unknown claims format",
					RequestContext: reqCtx,
				}
			}

			userIdVal, ok := claims["sub"]
			if !ok {
				return &AuthorizerError{
					Message:        "claims sub not found",
					RequestContext: reqCtx,
				}
			}

			userId, ok := userIdVal.(string)
			if !ok {
				return &AuthorizerError{
					Message:        "unknown claims sub format",
					RequestContext: reqCtx,
				}
			}

			cc.Custom["user-id"] = userId

			return nil
		})
	}
}

func SetLogger(logger Logger) GatewayOption {
	return func(s *Gateway) {
		if logger == nil {
			logger = &defaultLogger{}
		}
		s.logger = logger
	}
}

func NewLambdaGateway(lambdaClient *lambda.Lambda, opts ...GatewayOption) *Gateway {
	h := &Gateway{
		lambdaClient:         lambdaClient,
		functions:            map[string]map[string]function{},
		functionErrorHandler: func(err *LambdaError) error { return err },
		logger:               &defaultLogger{},
	}

	for _, f := range opts {
		f(h)
	}

	return h
}

func (s *Gateway) buildResponse(statusCode int, contentType, body string) *Response {
	headers := map[string]string{
		"Access-Control-Allow-Origin": "*",
	}

	if contentType != "" {
		headers["Content-Type"] = contentType
	}

	return &Response{
		StatusCode: statusCode,
		Headers:    headers,
		Body:       body,
	}
}

type ErrorMessage struct {
	ErrorType    string `json:"error_type"`
	ErrorMessage string `json:"error_message"`
}

type ErrorDecodeError struct {
	OriginalErrorMessage *ErrorMessage `json:"original_error_message"`
	Err                  error         `json:"err"`
}

func (e *ErrorDecodeError) Error() string {
	if e.Err == nil {
		return "ErrorDecodeError"
	}
	return e.Err.Error()
}

func (s *Gateway) errorResponse(e Error) *Response {
	msg := &ErrorMessage{
		ErrorType:    e.errorType(),
		ErrorMessage: e.errorMessage(),
	}

	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(msg); err != nil {
		s.logger.Error("Error decode", &ErrorDecodeError{
			OriginalErrorMessage: msg,
			Err:                  err,
		})
		return s.buildResponse(500, "application/json", `{"error_message":"Internal Server Error"}`)
	}

	return s.buildResponse(e.statusCode(), "application/josn", buf.String())
}

func getRequestContext(ctx *events.APIGatewayProxyRequestContext) (*requestcontext.Context, error) {
	requestId := ctx.RequestID

	claimsVal, ok := ctx.Authorizer["claims"]
	if !ok {
		return nil, &AuthorizerError{
			Message:        "claims not found",
			RequestContext: ctx,
		}
	}

	claims, ok := claimsVal.(map[string]interface{})
	if !ok {
		return nil, &AuthorizerError{
			Message:        "unknown claims format",
			RequestContext: ctx,
		}
	}

	userIdVal, ok := claims["sub"]
	if !ok {
		return nil, &AuthorizerError{
			Message:        "claims sub not found",
			RequestContext: ctx,
		}
	}

	userId, ok := userIdVal.(string)
	if !ok {
		return nil, &AuthorizerError{
			Message:        "unknown claims sub format",
			RequestContext: ctx,
		}
	}

	return &requestcontext.Context{
		RequestId: requestId,
		UserId:    userId,
	}, nil
}

func encodeClientContext(cc *lambdacontext.ClientContext) (string, error) {
	var buf strings.Builder

	w := base64.NewEncoder(base64.URLEncoding, &buf)

	enc := json.NewEncoder(w)

	if err := enc.Encode(cc); err != nil {
		return "", err
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (s *Gateway) Serve(ctx context.Context, req *Request) (*Response, error) {
	path, ok := s.functions[req.Path]
	if !ok {
		return s.errorResponse(ClientError(404, "NotFound")), nil
	}

	function, ok := path[req.HTTPMethod]
	if !ok {
		return s.errorResponse(ClientError(405, "MethodNotAllowed")), nil
	}

	clientContext := lambdacontext.ClientContext{
		Custom: map[string]string{},
	}
	for _, f := range s.contextModifiers {
		if err := f(&clientContext, req); err != nil {
			s.logger.Error("contextModifier", err)
			return s.errorResponse(ServerError()), nil
		}
	}

	cc, err := encodeClientContext(&clientContext)
	if err != nil {
		s.logger.Error("encode client_context", err)
		return s.errorResponse(ServerError()), nil
	}

	in := &lambda.InvokeInput{
		ClientContext:  aws.String(cc),
		FunctionName:   aws.String(string(function)),
		InvocationType: aws.String(lambda.InvocationTypeRequestResponse),
		Payload:        []byte(req.Body),
	}

	out, err := s.lambdaClient.InvokeWithContext(ctx, in)
	if err != nil {
		s.logger.Error("lambda invocation", err)
		return s.errorResponse(ServerError()), nil
	}

	if out.FunctionError != nil {
		buf := bytes.NewReader(out.Payload)
		d := json.NewDecoder(buf)
		errObj := &LambdaError{}
		if err := d.Decode(errObj); err != nil {
			s.logger.Error("DecodeError: "+string(out.Payload), err)
			return s.errorResponse(ServerError()), nil
		}

		err := s.functionErrorHandler(errObj)
		switch err.(type) {
		case *serverError:
			return s.errorResponse(err.(*serverError)), nil
		case *clientError:
			return s.errorResponse(err.(*clientError)), nil
		default:
			return s.errorResponse(ServerError()), nil
		}
	}

	var body, contentType string
	if out.Payload != nil {
		body = string(out.Payload)
		contentType = "application/json"
	}

	return s.buildResponse(200, contentType, body), nil
}
