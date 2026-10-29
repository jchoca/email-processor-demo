SHELL := /bin/bash

.PHONY: test build

up: down
	docker compose up -d --build

down:
	docker compose down --remove-orphans