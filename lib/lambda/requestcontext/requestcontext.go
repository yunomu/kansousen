package requestcontext

type Context struct {
	RequestId string
	UserId    string
}

const (
	RequestIdField = "api-request-id"
	UserIdField    = "user-id"
)

func FromCustomMap(in map[string]string) *Context {
	if in == nil {
		return nil
	}

	return &Context{
		RequestId: in[RequestIdField],
		UserId:    in[UserIdField],
	}
}
