.PHONY: build clean deploy proto proto-clean elm

SERVICES=kifu
PROTOBUF=document kifu
ELM_DIR=elm

TARGETS=$(addprefix bin/, $(SERVICES))
PROTO_TARGETS=$(addprefix proto/, $(PROTOBUF))
PROTO_ELM_TARGETS=$(ELM_DIR)/Proto/Kifu.proto

PUBLISH_DIR=public

build: proto
	sam build

bin/%: %/main.go
	env GOOS=linux go build -ldflags="-s -w" -o $@ $^

clean:
	rm -rf ./bin

deploy: build
	sam deploy

proto: $(PROTO_TARGETS) $(PROTO_ELM_TARGETS)

proto/%: proto/%.proto
	protoc --go_out=. $<

$(PROTO_ELM_TARGETS): proto/kifu.proto
	protoc --elm_out=$(ELM_DIR) $< 2> /dev/null
