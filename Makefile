SHELL := /bin/bash

.PHONY: test build up down demo

up: down
	docker compose up -d --build

down:
	docker compose down --remove-orphans

# send a few sample messages to a running stack
demo:
	@curl -s -o /dev/null -w '%{http_code} invoice\n' -X POST localhost:8080/v1/send-transactional -d '{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"name":"Alice","address":"alice@example.com"},"to":{"name":"Bob","address":"bob@example.com"},"subject":"Your invoice is ready","html":"<p>Thanks Bob</p>","text":"Thanks Bob","tags":["billing","vip"]}'
	@curl -s -o /dev/null -w '%{http_code} receipt\n' -X POST localhost:8080/v1/send-transactional -d '{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"name":"Alice","address":"alice@example.com"},"to":{"name":"Carol","address":"carol@example.com"},"subject":"Receipt for March","text":"Paid","tags":["billing"]}'
	@curl -s -o /dev/null -w '%{http_code} reset\n' -X POST localhost:8080/v1/send-transactional -d '{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"name":"Alice","address":"alice@example.com"},"to":{"name":"Bob","address":"bob@example.com"},"subject":"Reset your password","text":"Click here","tags":["security","transactional"]}'
