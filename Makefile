.PHONY: all proto proto-go proto-swift

all: proto

proto: proto-go proto-swift

proto-go:
	protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/*.proto

proto-swift:
	protoc --swift_out=. --grpc-swift-2_out=. proto/*.proto
