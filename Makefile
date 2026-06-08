.PHONY: build run test lint clean frontend backend dev

# Backend
build:
	cd inec-go-backend && go build -o ../bin/inec-backend .

run: build
	./bin/inec-backend

test:
	cd inec-go-backend && go test -v -count=1 ./...

lint:
	cd inec-go-backend && go vet ./...

backend-dev:
	cd inec-go-backend && go run .

# Frontend
frontend-install:
	cd inec-frontend && npm install

frontend-build:
	cd inec-frontend && npm run build

frontend-dev:
	cd inec-frontend && npm run dev

# Full stack
dev:
	@echo "Starting backend..."
	cd inec-go-backend && go run . &
	@echo "Starting frontend..."
	cd inec-frontend && npm run dev

clean:
	rm -rf bin/ inec-frontend/dist/
