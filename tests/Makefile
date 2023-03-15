all: docker generator runner

docker:
	docker build --tag eudico/e2e -f ./Dockerfile ../..

generator:
	go build -o build/generator ./generator

runner:
	go build -o build/runner ./runner

up:
	docker compose up -d

down:
	docker compose down

.PHONY: all node docker generator runner