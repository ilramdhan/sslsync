# sslsync — renew SSL certificates on all servers, safely.
#
#   make              show help
#
# Everything is configured in .env (see .env.example). These variables only
# select what a command works on:
#   ONLY=a,b          only these servers            make check ONLY=web-prod
#   EXCLUDE=a,b       all except these              make deploy EXCLUDE=db-prod
#   GROUP=staging     only servers in a group       make deploy GROUP=staging
#   ENV=.env          env file (repeat with ENV="a.env b.env")
#   PARALLEL=4        servers at the same time      YES=1  skip the deploy prompt
#   HOST=user@host    for find / k8s-ports          PORT=22  DOMAIN=example.com

BIN      := bin/sslsync
ENV      ?= .env
ONLY     ?=
EXCLUDE  ?=
GROUP    ?=
PARALLEL ?= 4
YES      ?=
HOST     ?=
PORT     ?= 22
DOMAIN   ?=

ENV_FLAGS := $(foreach f,$(ENV),-env $(f))
FILTER    := $(if $(ONLY),-only $(ONLY)) $(if $(EXCLUDE),-exclude $(EXCLUDE)) $(if $(GROUP),-group $(GROUP))
FLAGS     := $(ENV_FLAGS) -parallel $(PARALLEL) $(FILTER)
GO_SRC    := $(shell find cmd internal -name '*.go' -o -name '*.sh') go.mod go.sum
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X github.com/ilramdhan/sslsync/internal/app.Version=$(VERSION)
IMAGE     ?= ghcr.io/ilramdhan/sslsync
FUZZTIME  ?= 30s

.DEFAULT_GOAL := help
.PHONY: help setup build test fuzz lint validate list targets status plan check deploy \
        staging prod renew find k8s-ports trust-hosts logs last-log docker clean sync-template

##@ 1. Setup (once)

setup: ## Create .env from .env.example (chmod 600) and build
	@[ -f .env ] || { cp .env.example .env; echo "created .env — fill it in, then: make list"; }
	@chmod 600 .env
	@$(MAKE) --no-print-directory build

build: $(BIN) ## Build bin/sslsync
$(BIN): $(GO_SRC) .env.example
	@cmp -s .env.example internal/app/env.example || cp .env.example internal/app/env.example
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/sslsync

trust-hosts: build ## Save SSH host keys of servers not yet in known_hosts (shows fingerprint, asks)
	@$(BIN) trust $(FLAGS)

##@ 2. Inspect (read-only, nothing changes)

validate: build ## Local cert files: expiry, chain, key↔cert match, SAN covers every VERIFY host
	@$(BIN) validate $(FLAGS)

list: build ## How .env is read: servers, targets, files, verify ports (+ warnings for typos)
	@$(BIN) list $(FLAGS)

targets: build ## Built-in target types (nginx, minio, k8s, …) and their settings
	@$(BIN) targets

status: build ## NEW/OLD certificate on every VERIFY port right now (no SSH)
	@$(BIN) status $(FLAGS)

plan: build ## Dry run: what deploy would do, per server (no connection)
	@$(BIN) plan $(FLAGS)

check: build ## SSH pre-flight: connect, sudo, health, files, secrets, current TLS. Logged
	@$(BIN) check $(FLAGS)

##@ 3. Deploy (changes servers)

deploy: build ## check → upload → replace → reload → health → verify; rollback on failure. Logged
	@$(BIN) deploy $(FLAGS) $(if $(YES),-yes)

staging: ## deploy GROUP=staging
	@$(MAKE) --no-print-directory deploy GROUP=staging

prod: ## deploy GROUP=prod
	@$(MAKE) --no-print-directory deploy GROUP=prod

renew: build ## Guided: validate → check all → staging → status → (pause) → prod → status
	@$(BIN) validate $(ENV_FLAGS)
	@$(BIN) check $(ENV_FLAGS) -parallel $(PARALLEL)
	@$(BIN) deploy -group staging $(ENV_FLAGS) -parallel $(PARALLEL)
	@$(BIN) status -group staging $(ENV_FLAGS)
	@printf "\nStaging done. Check the apps in a browser, then continue to prod? [y/N] "; read ans; [ "$$ans" = y ]
	@$(BIN) deploy -group prod $(ENV_FLAGS) -parallel $(PARALLEL)
	@$(BIN) status $(ENV_FLAGS)

##@ 4. Discover (before adding a server)

find: build ## Scan a server: cert paths, TLS on every port, MinIO, k8s. HOST=user@host [DOMAIN=] [PORT=]
	@[ -n "$(HOST)" ] || { echo "usage: make find HOST=user@host [DOMAIN=example.com] [PORT=22]"; exit 1; }
	@mkdir -p reports
	@$(BIN) find $(HOST) $(DOMAIN) -p $(PORT) | tee reports/$(subst @,_,$(HOST)).txt

k8s-ports: ## Kubernetes: NodePort/LoadBalancer ports + TLS secrets. HOST=user@host
	@[ -n "$(HOST)" ] || { echo "usage: make k8s-ports HOST=deploy@k8s.example.com"; exit 1; }
	ssh -t -p $(PORT) $(HOST) 'sudo kubectl get svc -A | grep -E "NAMESPACE|NodePort|LoadBalancer"; echo; sudo kubectl get secret -A | grep -E "NAMESPACE|tls"'

##@ 5. Logs

logs: ## List runs, newest first, with ok/failed counts
	@[ -d logs ] || { echo "no logs yet"; exit 0; }; \
	for d in $$(ls -1dt logs/*/ 2>/dev/null); do \
		ok=$$(grep -c '"ok": true' $$d/summary.json 2>/dev/null); bad=$$(grep -c '"ok": false' $$d/summary.json 2>/dev/null); \
		printf "%-42s ok=%-3s failed=%s\n" "$$d" "$${ok:-?}" "$${bad:-?}"; done

last-log: ## Show the newest run.log
	@d=$$(ls -1dt logs/*/ 2>/dev/null | head -1); [ -n "$$d" ] && cat "$$d/run.log" || echo "no logs yet"

##@ 6. Development

test: sync-template ## go vet + unit tests (race detector)
	go vet ./... && go test -race ./...

sync-template: ## Copy .env.example into the binary (sslsync init uses it)
	@cmp -s .env.example internal/app/env.example || cp .env.example internal/app/env.example

fuzz: ## Run every fuzz target for FUZZTIME each (default 30s)
	go test ./internal/certs -run '^$$' -fuzz '^FuzzLoad$$' -fuzztime $(FUZZTIME)
	go test ./internal/inventory -run '^$$' -fuzz '^FuzzLoad$$' -fuzztime $(FUZZTIME)
	go test ./internal/remote -run '^$$' -fuzz '^FuzzShq$$' -fuzztime $(FUZZTIME)

lint: ## golangci-lint (config: .golangci.yml)
	golangci-lint run ./...

docker: ## Build the container image locally ($(IMAGE):$(VERSION))
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

clean: ## Remove bin/ (keeps logs, reports, .env, certs)
	rm -rf bin

##@ Help

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\n\033[1msslsync\033[0m — renew SSL certificates on all servers\n"} \
		/^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
	@printf "\n\033[1mFilters\033[0m  ONLY=a,b   EXCLUDE=a,b   GROUP=staging|prod   (any server command)\n"
	@printf "\n\033[1mRenewal, step by step\033[0m\n"
	@printf "  make validate                     1. new certificate files OK?\n"
	@printf "  make status                       2. what do the ports serve now?\n"
	@printf "  make check                        3. every server reachable + healthy? (read-only)\n"
	@printf "  make plan GROUP=staging           4. what will happen?\n"
	@printf "  make deploy ONLY=web-staging     5. one server first\n"
	@printf "  make staging                      6. rest of staging\n"
	@printf "  make prod                         7. production\n"
	@printf "  make status                       8. every port NEW?\n"
	@printf "  (or: make renew — 1, 3, 6, 8, pause, 7, 8)\n\n"
