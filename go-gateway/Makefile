.PHONY: build run test clean docker-build docker-run fmt

build:
	go build -o gateway .

run: build
	./gateway

test:
	go test -v ./...

test-cover:
	go test -cover ./...

benchmark:
	go test -bench=. -benchmem ./...

clean:
	rm -f gateway

fmt:
	go fmt ./...

lint:
	go vet ./...

docker-build:
	docker build -t go-llm-gateway:latest .

docker-run:
	docker-compose up -d

docker-stop:
	docker-compose down

docker-logs:
	docker-compose logs -f gateway

stats: build
	@echo "Binary size:"
	@ls -lh gateway | awk '{print "  " $$5}'
	@echo "Memory (if running):"
	@docker stats --no-stream go-llm-gateway-gateway-1 2>/dev/null | tail -1 || echo "  not running"
