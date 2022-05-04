package handler

import (
	"encoding/json"
)

type AppSyncIdentity string

const (
	AppSyncIdentityIAM     AppSyncIdentity = "AppSyncIdentityIAM"
	AppSyncIdentityCognito AppSyncIdentity = "AppSyncIdentityCognito"
	AppSyncIdentityOIDC    AppSyncIdentity = "AppSyncIdentityOIDC"
	AppSyncIdentityLambda  AppSyncIdentity = "AppSyncIdentityLambda"
)

type Request struct {
	Headers map[string]string `json:"headers,omitempty"`
}

type Info struct {
	SelectionSetList    []string               `json:"selectionSetList"`
	SelectionSetGraphQL string                 `json:"selectionSetGraphQL"`
	ParentTypeName      string                 `json:"parentTypeName"`
	FieldName           string                 `json:"fieldName"`
	Variables           map[string]interface{} `json:"variables"`
}

type Prev struct {
	Result map[string]interface{} `json:"result"`
}

type AppSyncResolverEvent struct {
	Arguments json.RawMessage        `json:"arguments"`
	Identity  AppSyncIdentity        `json:"identity,omitempty"`
	Source    map[string]interface{} `json:"source,omitempty"`
	Request   Request                `json:"request"`
	Info      Info                   `json:"info"`
	Prev      *Prev                  `json:"prev"`
	Stash     map[string]interface{} `json:"stash"`
}
