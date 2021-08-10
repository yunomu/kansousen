package requestcontext

type Context struct {
	RequestId string
	UserId    string
}

const (
	requestIdField = "api-request-id"
	userIdField    = "user-id"
)

func FromCustomMap(in map[string]string) *Context {
	if in == nil {
		return nil
	}

	return &Context{
		RequestId: in[requestIdField],
		UserId:    in[userIdField],
	}
}
