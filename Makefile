MONOFS_DIR ?= ../monofs
KVS_DIR ?= $(abspath $(MONOFS_DIR)/../kvs)

GUARDIAN_IMAGE ?= guardian:dev
GUARDIAN_CONTAINER ?= guardian

GUARDIAN_MONOFS_ROUTER ?= localhost:9090
GUARDIAN_MONOFS_TOKEN ?= guardian-dev-token
GUARDIAN_MONOFS_PRINCIPAL ?= guardiand
GUARDIAN_MONOFS_MOUNT_PATH ?= /
GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES ?= true

GUARDIAN_PRINCIPAL_ID ?= $(GUARDIAN_MONOFS_PRINCIPAL)
GUARDIAN_UI_LISTEN ?= 127.0.0.1:18090
GUARDIAN_UI_BASE_URL ?=
GUARDIAN_RECONCILE_INTERVAL ?= 1m
GUARDIAN_DEBOUNCE_MS ?= 250
GUARDIAN_PUSHERS ?= local:/.queues/local,docker-main:/.queues/docker-main,k8s-main:/.queues/k8s-main
GUARDIAN_OTEL_ENDPOINT ?= localhost:14317
GUARDIAN_OTEL_INSECURE ?= true
GUARDIAN_OTEL_SERVICE_NAME ?= guardian
GUARDIAN_OTEL_METRIC_INTERVAL ?= 15s

GUARDIAN_RUNTIME_DIR ?= /tmp/guardian-dogfood
GUARDIAN_BIN_DIR ?= $(GUARDIAN_RUNTIME_DIR)/bin
GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE ?= $(GUARDIAN_RUNTIME_DIR)/dogfood-monofs-encryption-key

GUARDIAN_DOCKER_PUSHER_NAME ?= docker-main
GUARDIAN_DOCKER_CLUSTER ?= docker-main
GUARDIAN_DOCKER_PUSHER_IMAGE ?= guardian-pusher-docker:dev
GUARDIAN_DOCKER_PUSHER_BINARY ?= $(GUARDIAN_BIN_DIR)/guardian-pusher-docker
GUARDIAN_DOCKER_PUSHER_PID_FILE ?= $(GUARDIAN_RUNTIME_DIR)/guardian-pusher-docker.pid
GUARDIAN_DOCKER_PUSHER_LOG ?= $(GUARDIAN_RUNTIME_DIR)/guardian-pusher-docker.log
GUARDIAN_DOCKER_STATE_DIR ?= $(GUARDIAN_RUNTIME_DIR)/guardian-pusher-docker-state
GUARDIAN_DOCKER_ADD_HOSTS ?= host.docker.internal:host-gateway

GUARDIAN_K8S_PUSHER_NAME ?= k8s-main
GUARDIAN_K8S_CLUSTER ?= k8s-main
GUARDIAN_K8S_PUSHER_IMAGE ?= guardian-pusher-k8s:dev
GUARDIAN_K8S_PUSHER_BINARY ?= $(GUARDIAN_BIN_DIR)/guardian-pusher-k8s
GUARDIAN_K8S_PUSHER_PID_FILE ?= $(GUARDIAN_RUNTIME_DIR)/guardian-pusher-k8s.pid
GUARDIAN_K8S_PUSHER_LOG ?= $(GUARDIAN_RUNTIME_DIR)/guardian-pusher-k8s.log
GUARDIAN_KUBECTL_BINARY ?= kubectl
GUARDIAN_KUBECONFIG ?=
GUARDIAN_KUBE_CONTEXT ?=

.DEFAULT_GOAL := help

.PHONY: help ui-build guardian-build guardian-up guardian-down guardian-restart guardian-logs \
	deploy-s3 deploy-s3-stop deploy-s3-clean \
	pusher-docker-build pusher-docker-image-build pusher-docker-up pusher-docker-down pusher-docker-logs \
	pusher-k8s-build pusher-k8s-image-build pusher-k8s-up pusher-k8s-down pusher-k8s-logs \
	dogfood-secret dogfood-push dogfood-up dogfood-deploy dogfood-status dogfood-stop dogfood-clean \
	bootstrap bootstrap-down

help:
	@printf "Usage:\n"
	@printf "  make bootstrap        Bootstrap the full stack: MonoFS + Guardian + base partitions\n"
	@printf "  make bootstrap-down   Tear down the bootstrap stack (pusher, Guardian, MonoFS)\n"
	@printf "  make deploy-s3        Start MonoFS deploy-s3 and Guardian in Docker\n"
	@printf "  make dogfood-up       Start MonoFS + Guardian, run the docker pusher, and push the example stack\n"
	@printf "  make dogfood-deploy   Alias for make dogfood-up\n"
	@printf "  make dogfood-status   Print the example intent states from MonoFS\n"
	@printf "  make dogfood-stop     Stop the dogfood pusher and the make deploy-s3 stack\n"
	@printf "  make dogfood-clean    Stop the dogfood pusher and clean the deploy-s3 stack\n"
	@printf "  make ui-build         Compile the TypeScript Guardian UI into static/app.js\n"
	@printf "  make guardian-up      Build and run Guardian in Docker only\n"
	@printf "  make pusher-docker-up Build and start the docker pusher against MonoFS\n"
	@printf "  make pusher-docker-image-build Build the guardian-pusher-docker Docker image\n"
	@printf "  make pusher-k8s-up    Build and start the kubernetes pusher against MonoFS\n"
	@printf "  make pusher-k8s-image-build Build the guardian-pusher-k8s Docker image\n"
	@printf "  make guardian-down    Stop the Guardian container\n"
	@printf "  make guardian-logs    Follow Guardian logs\n"
	@printf "  make deploy-s3-stop   Stop Guardian and stop MonoFS\n"
	@printf "  make deploy-s3-clean  Stop Guardian and clean MonoFS deploy-s3 data\n"
	@printf "\nConfigurable variables:\n"
	@printf "  MONOFS_DIR=%s\n" "$(MONOFS_DIR)"
	@printf "  GUARDIAN_MONOFS_ROUTER=%s\n" "$(GUARDIAN_MONOFS_ROUTER)"
	@printf "  GUARDIAN_MONOFS_TOKEN=%s\n" "$(GUARDIAN_MONOFS_TOKEN)"
	@printf "  GUARDIAN_UI_LISTEN=%s\n" "$(GUARDIAN_UI_LISTEN)"
	@printf "  GUARDIAN_UI_BASE_URL=%s\n" "$(GUARDIAN_UI_BASE_URL)"
	@printf "  GUARDIAN_OTEL_ENDPOINT=%s\n" "$(GUARDIAN_OTEL_ENDPOINT)"
	@printf "  GUARDIAN_DOCKER_ADD_HOSTS=%s\n" "$(GUARDIAN_DOCKER_ADD_HOSTS)"
	@printf "  MONOFS_ENCRYPTION_KEY=<optional override for dogfood-up>\n"
	@printf "  GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE=%s\n" "$(GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE)"

$(GUARDIAN_BIN_DIR):
	mkdir -p "$(GUARDIAN_BIN_DIR)"

ui-build:
	npm run build:ui

guardian-build:
	@if [ ! -d "$(MONOFS_DIR)" ]; then echo "MonoFS repo not found at $(MONOFS_DIR)"; exit 1; fi
	@if [ ! -d "$(KVS_DIR)" ]; then echo "KVS repo not found at $(KVS_DIR)"; exit 1; fi
	DOCKER_BUILDKIT=1 docker build --build-context monofs=$(abspath $(MONOFS_DIR)) --build-context kvs=$(KVS_DIR) -t $(GUARDIAN_IMAGE) .


deploy-s3:
	@if [ ! -d "$(MONOFS_DIR)" ]; then echo "MonoFS repo not found at $(MONOFS_DIR)"; exit 1; fi
	$(MAKE) -s -C $(MONOFS_DIR) deploy-s3
	$(MAKE) guardian-up

deploy-s3-stop:
	@if [ ! -d "$(MONOFS_DIR)" ]; then echo "MonoFS repo not found at $(MONOFS_DIR)"; exit 1; fi
	$(MAKE) guardian-down
	$(MAKE) -C $(MONOFS_DIR) deploy-stop

deploy-s3-clean:
	@if [ ! -d "$(MONOFS_DIR)" ]; then echo "MonoFS repo not found at $(MONOFS_DIR)"; exit 1; fi
	$(MAKE) guardian-down
	$(MAKE) -C $(MONOFS_DIR) deploy-s3-clean

pusher-docker-build: $(GUARDIAN_BIN_DIR)
	go build -o "$(GUARDIAN_DOCKER_PUSHER_BINARY)" ./cmd/guardian-pusher-docker

pusher-docker-image-build:
	@if [ ! -d "$(MONOFS_DIR)" ]; then echo "MonoFS repo not found at $(MONOFS_DIR)"; exit 1; fi
	@if [ ! -d "$(KVS_DIR)" ]; then echo "KVS repo not found at $(KVS_DIR)"; exit 1; fi
	DOCKER_BUILDKIT=1 docker build --build-context monofs=$(abspath $(MONOFS_DIR)) --build-context kvs=$(KVS_DIR) -f Dockerfile.pusher-docker -t $(GUARDIAN_DOCKER_PUSHER_IMAGE) .

pusher-docker-up: pusher-docker-build
	@mkdir -p "$(GUARDIAN_RUNTIME_DIR)" "$(GUARDIAN_DOCKER_STATE_DIR)"
	@if [ -f "$(GUARDIAN_DOCKER_PUSHER_PID_FILE)" ]; then \
		pid=$$(cat "$(GUARDIAN_DOCKER_PUSHER_PID_FILE)"); \
		if kill -0 $$pid 2>/dev/null; then \
			echo "Docker pusher already running (pid $$pid)."; \
			exit 0; \
		fi; \
		rm -f "$(GUARDIAN_DOCKER_PUSHER_PID_FILE)"; \
	fi
	@GUARDIAN_OTEL_ENDPOINT="$(GUARDIAN_OTEL_ENDPOINT)" \
		GUARDIAN_OTEL_INSECURE="$(GUARDIAN_OTEL_INSECURE)" \
		GUARDIAN_OTEL_SERVICE_NAME="$(GUARDIAN_OTEL_SERVICE_NAME)" \
		GUARDIAN_OTEL_METRIC_INTERVAL="$(GUARDIAN_OTEL_METRIC_INTERVAL)" \
		nohup "$(GUARDIAN_DOCKER_PUSHER_BINARY)" \
		--pusher-name "$(GUARDIAN_DOCKER_PUSHER_NAME)" \
		--cluster "$(GUARDIAN_DOCKER_CLUSTER)" \
		--monofs-router "$(GUARDIAN_MONOFS_ROUTER)" \
		--monofs-token "$(GUARDIAN_MONOFS_TOKEN)" \
		--monofs-use-external-addresses="$(GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES)" \
		$(if $(strip $(GUARDIAN_DOCKER_ADD_HOSTS)),--docker-add-hosts "$(GUARDIAN_DOCKER_ADD_HOSTS)",) \
		--docker-state-dir "$(GUARDIAN_DOCKER_STATE_DIR)" \
		>"$(GUARDIAN_DOCKER_PUSHER_LOG)" 2>&1 & echo $$! >"$(GUARDIAN_DOCKER_PUSHER_PID_FILE)"
	@echo "Docker pusher started. Logs: $(GUARDIAN_DOCKER_PUSHER_LOG)"

pusher-docker-down:
	@set -e; \
	if [ ! -f "$(GUARDIAN_DOCKER_PUSHER_PID_FILE)" ]; then \
		echo "Docker pusher is not running."; \
	else \
		pid=$$(cat "$(GUARDIAN_DOCKER_PUSHER_PID_FILE)"); \
		if kill -0 $$pid 2>/dev/null; then \
			kill $$pid; \
			for _ in 1 2 3 4 5 6 7 8 9 10; do \
				if ! kill -0 $$pid 2>/dev/null; then \
					break; \
				fi; \
				sleep 1; \
			done; \
			if kill -0 $$pid 2>/dev/null; then \
				echo "Docker pusher did not stop cleanly (pid $$pid)."; \
				exit 1; \
			fi; \
		fi; \
		rm -f "$(GUARDIAN_DOCKER_PUSHER_PID_FILE)"; \
		echo "Docker pusher stopped."; \
	fi

pusher-docker-logs:
	tail -f "$(GUARDIAN_DOCKER_PUSHER_LOG)"

pusher-k8s-build: $(GUARDIAN_BIN_DIR)
	go build -o "$(GUARDIAN_K8S_PUSHER_BINARY)" ./cmd/guardian-pusher-k8s

pusher-k8s-image-build:
	@if [ ! -d "$(MONOFS_DIR)" ]; then echo "MonoFS repo not found at $(MONOFS_DIR)"; exit 1; fi
	@if [ ! -d "$(KVS_DIR)" ]; then echo "KVS repo not found at $(KVS_DIR)"; exit 1; fi
	DOCKER_BUILDKIT=1 docker build --build-context monofs=$(abspath $(MONOFS_DIR)) --build-context kvs=$(KVS_DIR) -f Dockerfile.pusher-k8s -t $(GUARDIAN_K8S_PUSHER_IMAGE) .

pusher-k8s-up: pusher-k8s-build
	@mkdir -p "$(GUARDIAN_RUNTIME_DIR)"
	@command -v "$(GUARDIAN_KUBECTL_BINARY)" >/dev/null 2>&1 || { echo "kubectl binary not found: $(GUARDIAN_KUBECTL_BINARY)"; exit 1; }
	@if [ -f "$(GUARDIAN_K8S_PUSHER_PID_FILE)" ]; then \
		pid=$$(cat "$(GUARDIAN_K8S_PUSHER_PID_FILE)"); \
		if kill -0 $$pid 2>/dev/null; then \
			echo "Kubernetes pusher already running (pid $$pid)."; \
			exit 0; \
		fi; \
		rm -f "$(GUARDIAN_K8S_PUSHER_PID_FILE)"; \
	fi
	@GUARDIAN_OTEL_ENDPOINT="$(GUARDIAN_OTEL_ENDPOINT)" \
		GUARDIAN_OTEL_INSECURE="$(GUARDIAN_OTEL_INSECURE)" \
		GUARDIAN_OTEL_SERVICE_NAME="$(GUARDIAN_OTEL_SERVICE_NAME)" \
		GUARDIAN_OTEL_METRIC_INTERVAL="$(GUARDIAN_OTEL_METRIC_INTERVAL)" \
		nohup "$(GUARDIAN_K8S_PUSHER_BINARY)" \
		--pusher-name "$(GUARDIAN_K8S_PUSHER_NAME)" \
		--cluster "$(GUARDIAN_K8S_CLUSTER)" \
		--monofs-router "$(GUARDIAN_MONOFS_ROUTER)" \
		--monofs-token "$(GUARDIAN_MONOFS_TOKEN)" \
		--monofs-use-external-addresses="$(GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES)" \
		--kubectl-binary "$(GUARDIAN_KUBECTL_BINARY)" \
		$(if $(strip $(GUARDIAN_KUBECONFIG)),--kubeconfig "$(GUARDIAN_KUBECONFIG)",) \
		$(if $(strip $(GUARDIAN_KUBE_CONTEXT)),--kube-context "$(GUARDIAN_KUBE_CONTEXT)",) \
		>"$(GUARDIAN_K8S_PUSHER_LOG)" 2>&1 & echo $$! >"$(GUARDIAN_K8S_PUSHER_PID_FILE)"
	@echo "Kubernetes pusher started. Logs: $(GUARDIAN_K8S_PUSHER_LOG)"

pusher-k8s-down:
	@set -e; \
	if [ ! -f "$(GUARDIAN_K8S_PUSHER_PID_FILE)" ]; then \
		echo "Kubernetes pusher is not running."; \
	else \
		pid=$$(cat "$(GUARDIAN_K8S_PUSHER_PID_FILE)"); \
		if kill -0 $$pid 2>/dev/null; then \
			kill $$pid; \
			for _ in 1 2 3 4 5 6 7 8 9 10; do \
				if ! kill -0 $$pid 2>/dev/null; then \
					break; \
				fi; \
				sleep 1; \
			done; \
			if kill -0 $$pid 2>/dev/null; then \
				echo "Kubernetes pusher did not stop cleanly (pid $$pid)."; \
				exit 1; \
			fi; \
		fi; \
		rm -f "$(GUARDIAN_K8S_PUSHER_PID_FILE)"; \
		echo "Kubernetes pusher stopped."; \
	fi

pusher-k8s-logs:
	tail -f "$(GUARDIAN_K8S_PUSHER_LOG)"

dogfood-secret:
	@mkdir -p "$(GUARDIAN_RUNTIME_DIR)"
	@if [ -n "$(MONOFS_ENCRYPTION_KEY)" ]; then \
		printf '%s' "$(MONOFS_ENCRYPTION_KEY)" >"$(GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE)" && \
		chmod 600 "$(GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE)" && \
		echo "Dogfood encryption key refreshed from MONOFS_ENCRYPTION_KEY."; \
	elif [ -f "$(GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE)" ]; then \
		echo "Dogfood encryption key already present at $(GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE)."; \
	else \
		od -An -N 32 -tx1 /dev/urandom | tr -d ' \n' >"$(GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE)" && \
		chmod 600 "$(GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE)" && \
		echo "Dogfood encryption key generated at $(GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE)."; \
	fi

dogfood-push: dogfood-secret
	@tmp_bundle_dir=$$(mktemp -d); \
	trap 'rm -rf "$$tmp_bundle_dir"' EXIT; \
	cp -R ./partitions/guardian-local/. "$$tmp_bundle_dir"/ && \
	mkdir -p "$$tmp_bundle_dir/secrets" && \
	cp "$(GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE)" "$$tmp_bundle_dir/secrets/encryption-key" && \
	go run ./cmd/guardianctl --monofs-router "$(GUARDIAN_MONOFS_ROUTER)" --monofs-token "$(GUARDIAN_MONOFS_TOKEN)" $(if $(filter true TRUE 1 yes YES,$(GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES)),--monofs-use-external-addresses,) partition push --include-secrets --dir "$$tmp_bundle_dir" && \
	go run ./cmd/guardianctl --monofs-router "$(GUARDIAN_MONOFS_ROUTER)" --monofs-token "$(GUARDIAN_MONOFS_TOKEN)" $(if $(filter true TRUE 1 yes YES,$(GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES)),--monofs-use-external-addresses,) partition push --dir ./partitions/minio-local && \
	go run ./cmd/guardianctl --monofs-router "$(GUARDIAN_MONOFS_ROUTER)" --monofs-token "$(GUARDIAN_MONOFS_TOKEN)" $(if $(filter true TRUE 1 yes YES,$(GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES)),--monofs-use-external-addresses,) partition push --dir ./partitions/monofs-local

dogfood-up: dogfood-secret
	@key=$$(cat "$(GUARDIAN_DOGFOOD_ENCRYPTION_KEY_FILE)") && \
	$(MAKE) MONOFS_ENCRYPTION_KEY="$$key" deploy-s3 && \
	$(MAKE) pusher-docker-image-build && \
	$(MAKE) pusher-docker-up && \
	$(MAKE) dogfood-push && \
	echo "Dogfood stack submitted. Check Guardian at http://$(GUARDIAN_UI_LISTEN) or run make dogfood-status."

dogfood-deploy: dogfood-up

dogfood-status:
	go run ./cmd/guardianctl --monofs-router "$(GUARDIAN_MONOFS_ROUTER)" --monofs-token "$(GUARDIAN_MONOFS_TOKEN)" $(if $(filter true TRUE 1 yes YES,$(GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES)),--monofs-use-external-addresses,) intent list --partition guardian-local
	go run ./cmd/guardianctl --monofs-router "$(GUARDIAN_MONOFS_ROUTER)" --monofs-token "$(GUARDIAN_MONOFS_TOKEN)" $(if $(filter true TRUE 1 yes YES,$(GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES)),--monofs-use-external-addresses,) intent list --partition minio-local
	go run ./cmd/guardianctl --monofs-router "$(GUARDIAN_MONOFS_ROUTER)" --monofs-token "$(GUARDIAN_MONOFS_TOKEN)" $(if $(filter true TRUE 1 yes YES,$(GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES)),--monofs-use-external-addresses,) intent list --partition monofs-local

dogfood-stop: pusher-docker-down deploy-s3-stop

dogfood-clean: pusher-docker-down deploy-s3-clean

bootstrap:
	bash ../scripts/onboard.sh bootstrap

bootstrap-down: pusher-docker-down guardian-down
	$(MAKE) -C $(MONOFS_DIR) deploy-s3-clean
