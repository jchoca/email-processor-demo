SHELL := /bin/bash

.PHONY: test build up down demo load

up: down
	docker compose up -d --build

down:
	docker compose down --remove-orphans

down-v:
	docker compose down -v --remove-orphans

# send a few sample messages to a running stack
demo:
	@curl -s -o /dev/null -w '%{http_code} invoice\n' -X POST localhost:8080/v1/send-transactional -d '{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"name":"Alice","address":"alice@example.com"},"to":{"name":"Bob","address":"bob@example.com"},"subject":"Your invoice is ready","html":"<p>Thanks Bob</p>","text":"Thanks Bob","tags":["billing","vip"]}'
	@curl -s -o /dev/null -w '%{http_code} receipt\n' -X POST localhost:8080/v1/send-transactional -d '{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"name":"Alice","address":"alice@example.com"},"to":{"name":"Carol","address":"carol@example.com"},"subject":"Receipt for March","text":"Paid","tags":["billing"]}'
	@curl -s -o /dev/null -w '%{http_code} reset\n' -X POST localhost:8080/v1/send-transactional -d '{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"name":"Alice","address":"alice@example.com"},"to":{"name":"Bob","address":"bob@example.com"},"subject":"Reset your password","text":"Click here","tags":["security","transactional"]}'

# requests per second for `make load`
RATE ?= 5

# send a steady stream of requests to a running stack until Ctrl-C; curl runs
# in the background so a slow response does not lower the rate
load:
	@echo "sending $(RATE)/s to localhost:8080, Ctrl-C to stop"
	@accounts=(0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10 7d2e4a90-1c3b-4f5e-8a6d-9b0c1e2f3a4b); \
	while true; do \
	  acct=$${accounts[RANDOM % $${#accounts[@]}]}; \
	  curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8080/v1/send-transactional \
	    -d '{"accountId":"'$$acct'","from":{"address":"alice@example.com"},"to":{"address":"bob@example.com"},"subject":"load test","text":"hi","tags":["load"]}' & \
	  sleep $(shell awk 'BEGIN{print 1/$(RATE)}'); \
	done
