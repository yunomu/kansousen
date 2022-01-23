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
)

type Request events.APIGatewayV2HTTPRequest
type Response events.APIGatewayV2HTTPResponse

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

type function struct {
	lambdaArn string
	id        string
}

type Gateway struct {
	lambdaClient         *lambda.Lambda
	basePath             string
	functions            map[string]map[string]function
	functionErrorHandler func(*LambdaError) error
	contextModifiers     []func(*lambdacontext.ClientContext, *Request) error
	logger               Logger
}

type GatewayOption func(*Gateway)

func AddFunction(path, method, lambdaArn string, id string) GatewayOption {
	return func(s *Gateway) {
		p, ok := s.functions[path]
		if !ok {
			p = map[string]function{}
		}

		p[method] = function{
			lambdaArn: lambdaArn,
			id:        id,
		}
		s.functions[path] = p
	}
}

func SetFunctionErrorHandler(h func(*LambdaError) error) GatewayOption {
	return func(s *Gateway) {
		if h == nil {
			h = func(err *LambdaError) error { return err }
		}
		s.functionErrorHandler = h
	}
}

func WithAPIRequestID(apiRequestIdField string) GatewayOption {
	return func(s *Gateway) {
		s.contextModifiers = append(s.contextModifiers, func(cc *lambdacontext.ClientContext, req *Request) error {
			if cc == nil {
				return nil
			}

			if cc.Custom == nil {
				cc.Custom = make(map[string]string)
			}

			cc.Custom[apiRequestIdField] = req.RequestContext.RequestID

			return nil
		})
	}
}

func SetBasePath(basePath string) GatewayOption {
	return func(s *Gateway) {
		s.basePath = basePath
	}
}

type AuthorizerError struct {
	Message        string                                 `json:"message"`
	RequestContext *events.APIGatewayV2HTTPRequestContext `json:"request_context"`
}

func (e *AuthorizerError) Error() string {
	return e.Message
}

func WithClaimSubID(field string) GatewayOption {
	return func(s *Gateway) {
		s.contextModifiers = append(s.contextModifiers, func(cc *lambdacontext.ClientContext, req *Request) error {
			if cc == nil {
				return nil
			}

			if cc.Custom == nil {
				cc.Custom = make(map[string]string)
			}

			reqCtx := &req.RequestContext
			jwt := reqCtx.Authorizer.JWT

			subId, ok := jwt.Claims["sub"]
			if !ok {
				return &AuthorizerError{
					Message:        "unknown claims sub format",
					RequestContext: reqCtx,
				}
			}

			cc.Custom[field] = subId

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

func NewLambdaGateway(
	lambdaClient *lambda.Lambda,
	opts ...GatewayOption,
) *Gateway {
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
	reqPath := strings.TrimPrefix(req.RawPath, s.basePath)

	path, ok := s.functions[reqPath]
	if !ok {
		return s.errorResponse(ClientError(404, "NotFound")), nil
	}

	function, ok := path[req.RequestContext.HTTP.Method]
	if !ok {
		return s.errorResponse(ClientError(405, "MethodNotAllowed")), nil
	}

	clientContext := lambdacontext.ClientContext{
		Custom: map[string]string{
			"function-id": function.id,
		},
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
		FunctionName:   aws.String(function.lambdaArn),
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
