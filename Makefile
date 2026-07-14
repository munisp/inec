.PHONY: all build test clean run

all: build

build:
cd inec-go-backend && go build -o inec-backend

test:
cd inec-go-backend && go test ./...

clean:
rm -f inec-go-backend/inec-backend

run: build
cd inec-go-backend && ./inec-backend
