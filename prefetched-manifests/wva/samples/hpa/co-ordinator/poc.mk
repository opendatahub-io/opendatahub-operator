##@ POC Demo (Coordinator GPU Rebalance)

POC_NS       ?= llm-d-sim
POC_DIR      := config/samples/hpa/co-ordinator
POC_WVA_NS  := workload-variant-autoscaler-system
POC_MON_NS  := workload-variant-autoscaler-monitoring
GAIE_VERSION ?= v1.5.0
LLM_D_ROUTER_VERSION ?= v0.9.0

.PHONY: poc-install
poc-install: ## [POC] Install per-model EPPs, sim workloads, gateway, and Prometheus Adapter rules
	@echo ""
	@echo "================================================================"
	@echo "  POC Install: model-a + model-b EPPs, workloads, gateway"
	@echo "================================================================"
	@echo ""
	@echo "--- Step 1: Remove default optimized-baseline EPP ---"
	@helm uninstall optimized-baseline -n $(POC_NS) 2>/dev/null || true
	@helm uninstall model-a -n $(POC_NS) 2>/dev/null || true
	@helm uninstall model-b -n $(POC_NS) 2>/dev/null || true
	@kubectl delete clusterrolebinding optimized-baseline-epp-tokenreview \
	  --ignore-not-found=true 2>/dev/null; true
	@kubectl delete clusterrole optimized-baseline-epp-tokenreview \
	  --ignore-not-found=true 2>/dev/null; true
	@kubectl delete configmap envoy -n $(POC_NS) --ignore-not-found=true 2>/dev/null; true
	@echo "  done"
	@echo ""
	@echo "--- Step 2: Install model-a EPP (flowControl enabled) ---"
	@helm upgrade --install model-a \
	  oci://ghcr.io/llm-d/charts/llm-d-router-standalone \
	  --version $(LLM_D_ROUTER_VERSION) \
	  --namespace $(POC_NS) \
	  -f $(POC_DIR)/model-a-epp-values.yaml
	@echo ""
	@echo "--- Step 3: Install model-b EPP (flowControl enabled) ---"
	@kubectl delete configmap envoy -n $(POC_NS) --ignore-not-found=true 2>/dev/null; true
	@helm upgrade --install model-b \
	  oci://ghcr.io/llm-d/charts/llm-d-router-standalone \
	  --version $(LLM_D_ROUTER_VERSION) \
	  --namespace $(POC_NS) \
	  -f $(POC_DIR)/model-b-epp-values.yaml
	@echo ""
	@echo "--- Step 4: Deploy sim workloads and gateway ---"
	@kubectl apply -k $(POC_DIR)/base
	@echo ""
	@echo "--- Step 5: Wait for EPPs and workloads to be ready ---"
	@echo "  Waiting for model-a-epp..."
	@kubectl rollout status deployment/model-a-epp -n $(POC_NS) --timeout=180s
	@echo "  Waiting for model-b-epp..."
	@kubectl rollout status deployment/model-b-epp -n $(POC_NS) --timeout=180s
	@echo "  Waiting for model-a-decode..."
	@kubectl rollout status deployment/model-a-decode -n $(POC_NS) --timeout=120s
	@echo "  Waiting for model-b-decode..."
	@kubectl rollout status deployment/model-b-decode -n $(POC_NS) --timeout=120s
	@echo "  Waiting for multi-model-gateway..."
	@kubectl rollout status deployment/multi-model-gateway -n $(POC_NS) --timeout=60s
	@echo "  Restarting gateway so nginx re-resolves EPP service IPs..."
	@kubectl rollout restart deployment/multi-model-gateway -n $(POC_NS)
	@kubectl rollout status deployment/multi-model-gateway -n $(POC_NS) --timeout=60s
	@echo ""
	@echo "--- Step 6: Add EPP queue rules to Prometheus Adapter ---"
	@helm upgrade prometheus-adapter prometheus-community/prometheus-adapter \
	  -n $(POC_MON_NS) \
	  --reuse-values \
	  -f $(POC_DIR)/prometheus-adapter-epp-rules.yaml
	@kubectl rollout restart deployment/prometheus-adapter -n $(POC_MON_NS)
	@kubectl rollout status deployment/prometheus-adapter -n $(POC_MON_NS) --timeout=180s
	@echo ""
	@echo "--- Step 7: Prime EPP flow control metrics ---"
	@echo "  Sending one priming request per EPP (metric is not registered until first request)..."
	@kubectl port-forward -n $(POC_NS) svc/model-a-epp 18880:80 &>/dev/null & \
	  PF_A=$$!; sleep 3; \
	  curl -s -X POST http://localhost:18880/v1/completions \
	    -H "Content-Type: application/json" \
	    -d '{"model":"model-a","prompt":"hi","max_tokens":1}' >/dev/null 2>&1 || true; \
	  kill $$PF_A 2>/dev/null; true
	@kubectl port-forward -n $(POC_NS) svc/model-b-epp 18881:80 &>/dev/null & \
	  PF_B=$$!; sleep 3; \
	  curl -s -X POST http://localhost:18881/v1/completions \
	    -H "Content-Type: application/json" \
	    -d '{"model":"model-b","prompt":"hi","max_tokens":1}' >/dev/null 2>&1 || true; \
	  kill $$PF_B 2>/dev/null; true
	@echo "  Waiting 30s for Prometheus to scrape the new metrics..."
	@sleep 30
	@echo ""
	@echo "--- Step 8: Verify external metrics are reachable ---"
	@for metric in model_a_epp_queue_size model_b_epp_queue_size; do \
	  val=$$(kubectl get --raw \
	    "/apis/external.metrics.k8s.io/v1beta1/namespaces/$(POC_NS)/$$metric" \
	    2>/dev/null); \
	  if echo "$$val" | grep -q '"value"'; then \
	    echo "  $$metric: READY"; \
	  else \
	    echo "  $$metric: NOT READY (run 'make poc-status' in ~60s once Prometheus scrapes the EPPs)"; \
	  fi; \
	done
	@echo ""
	@echo "================================================================"
	@echo "  Install complete. Run 'make poc-status' to verify."
	@echo "================================================================"
	@echo ""

.PHONY: poc-enable-coordinator
poc-enable-coordinator: ## [POC] Enable the Coordinator (experimental feature; disabled by default in manager-configmap.yaml)
	@echo ""
	@echo "--- Enabling EXPERIMENTAL_COORDINATOR_ENABLED in wva-manager-config ---"
	@kubectl get configmap wva-manager-config -n $(POC_WVA_NS) -o yaml \
	  | sed 's/EXPERIMENTAL_COORDINATOR_ENABLED: "false"/EXPERIMENTAL_COORDINATOR_ENABLED: "true"/' \
	  | kubectl apply -f -
	@echo ""
	@echo "--- Restarting WVA controller to pick up new config ---"
	@kubectl rollout restart deployment \
	  -n $(POC_WVA_NS) \
	  -l app.kubernetes.io/name=workload-variant-autoscaler
	@kubectl rollout status deployment \
	  -n $(POC_WVA_NS) \
	  -l app.kubernetes.io/name=workload-variant-autoscaler \
	  --timeout=60s
	@echo ""
	@echo "--- Coordinator enabled ---"
	@kubectl get configmap wva-manager-config -n $(POC_WVA_NS) \
	  -o jsonpath='{.data.config\.yaml}' 2>/dev/null \
	  | grep -E "COORDINATOR" | sed 's/^/  /'
	@echo ""

.PHONY: poc-status
poc-status: ## [POC] Status report: cluster health, WVA, HPAs, ResourceQuota, Coordinator
	@echo ""
	@echo "================================================================"
	@echo "  POC Status"
	@echo "================================================================"
	@echo ""
	@echo "=== Node GPU capacity ==="
	@kubectl get node \
	  -o custom-columns='NODE:.metadata.name,GPU:.status.allocatable.nvidia\.com/gpu' \
	  2>/dev/null || echo "  ERROR: cannot reach cluster"
	@echo ""
	@echo "=== WVA controller ($(POC_WVA_NS)) ==="
	@kubectl get pods -n $(POC_WVA_NS) \
	  -l app.kubernetes.io/name=workload-variant-autoscaler \
	  --no-headers 2>/dev/null \
	  | awk '{printf "  %-50s %s\n", $$1, $$3}' \
	  || echo "  MISSING"
	@echo ""
	@echo "=== Coordinator config ==="
	@kubectl get configmap wva-manager-config -n $(POC_WVA_NS) \
	  -o jsonpath='{.data.config\.yaml}' 2>/dev/null \
	  | grep -E "COORDINATOR" | sed 's/^/  /' \
	  || echo "  MISSING: wva-manager-config"
	@echo ""
	@echo "=== ResourceQuota ($(POC_NS)) ==="
	@kubectl get resourcequota -n $(POC_NS) 2>/dev/null \
	  || echo "  NONE — Coordinator will skip (no GPU quota to split)"
	@echo ""
	@echo "=== HPAs ($(POC_NS)) ==="
	@kubectl get hpa -n $(POC_NS) \
	  -o custom-columns='HPA:.metadata.name,MAX:.spec.maxReplicas,CURRENT:.status.currentReplicas,DESIRED:.status.desiredReplicas' \
	  2>/dev/null || echo "  NONE"
	@echo ""
	@echo "=== Pods ($(POC_NS)) ==="
	@kubectl get pods -n $(POC_NS) --no-headers 2>/dev/null \
	  | grep "model-[ab]-decode" \
	  | awk '{printf "  %-50s %s\n", $$1, $$3}' \
	  || echo "  NONE"
	@PEND=$$(kubectl get pods -n $(POC_NS) \
	    --field-selector=status.phase=Pending --no-headers 2>/dev/null \
	    | grep "model-[ab]-decode" | wc -l | tr -d ' '); \
	  if [ "$$PEND" -gt 0 ]; then \
	    echo ""; \
	    echo "  *** $$PEND pod(s) PENDING — starvation is active ***"; \
	    kubectl get pods -n $(POC_NS) --field-selector=status.phase=Pending \
	      --no-headers 2>/dev/null \
	      | grep "model-[ab]-decode" | awk '{printf "  %-50s %s\n", $$1, $$3}'; \
	  fi
	@echo ""
	@echo "=== Recent Coordinator decisions ==="
	@kubectl logs -n $(POC_WVA_NS) \
	  -l app.kubernetes.io/name=workload-variant-autoscaler \
	  --tail=40 2>/dev/null \
	  | grep -iE "gpu-rebalance|Rebalancing|contention|coordinator" \
	  | tail -5 | sed 's/^/  /' \
	  || echo "  (none)"
	@echo ""

.PHONY: poc-starvation
poc-starvation: ## [POC] Reproduce GPU starvation: reset → phase-1 model-a load → phase-2 add model-b
	@echo ""
	@echo "================================================================"
	@echo "  POC Demo: GPU Starvation"
	@echo "================================================================"
	@echo ""
	@echo "--- Step 1: Reset to clean state ---"
	@echo "  Removing stale load jobs..."
	@kubectl delete job starvation-load-a starvation-load-b \
	  -n $(POC_NS) --ignore-not-found=true 2>/dev/null; true
	@echo "  Removing existing HPAs (clears stabilisation history)..."
	@kubectl delete hpa model-a-hpa model-b-hpa -n $(POC_NS) --ignore-not-found=true 2>/dev/null; true
	@echo "  Scaling both deployments to 1..."
	@kubectl scale deployment model-a-decode model-b-decode --replicas=1 -n $(POC_NS)
	@echo "  Waiting for both deployments to reach 1 ready replica (timeout 120s)..."
	@T=0; \
	  while true; do \
	    A=$$(kubectl get deployment model-a-decode -n $(POC_NS) \
	      -o jsonpath='{.status.readyReplicas}' 2>/dev/null); \
	    B=$$(kubectl get deployment model-b-decode -n $(POC_NS) \
	      -o jsonpath='{.status.readyReplicas}' 2>/dev/null); \
	    if [ "$$A" = "1" ] && [ "$$B" = "1" ]; then \
	      echo "  Both at 1 ready replica"; break; \
	    fi; \
	    T=$$((T+5)); \
	    if [ $$T -ge 120 ]; then echo "  TIMEOUT waiting for scale-down"; exit 1; fi; \
	    echo "  [$$T""s] waiting... model-a=$$A model-b=$$B"; \
	    sleep 5; \
	  done
	@echo "  Applying base state (quota + HPAs without coordinator annotation)..."
	@kubectl apply -k $(POC_DIR)/base
	@echo ""
	@echo "--- Baseline (1 pod each, 2/10 GPUs used) ---"
	@kubectl get hpa -n $(POC_NS) \
	  -o custom-columns='HPA:.metadata.name,MAX:.spec.maxReplicas,CURRENT:.status.currentReplicas'
	@echo ""
	@echo "--- Phase 1: Heavy load on model-a only (model-b stays idle) ---"
	@echo "  Starting load job (80 concurrent burst + 10 req/s drip for 4 min)..."
	@kubectl apply -f $(POC_DIR)/starvation-load-a.yaml
	@echo "  Watching model-a scale up (90s, 10s intervals)..."
	@echo ""
	@for i in $$(seq 1 9); do \
	  sleep 10; \
	  A_MAX=$$(kubectl get hpa model-a-hpa -n $(POC_NS) \
	    -o jsonpath='{.spec.maxReplicas}' 2>/dev/null); \
	  A_RUN=$$(kubectl get hpa model-a-hpa -n $(POC_NS) \
	    -o jsonpath='{.status.currentReplicas}' 2>/dev/null); \
	  B_RUN=$$(kubectl get hpa model-b-hpa -n $(POC_NS) \
	    -o jsonpath='{.status.currentReplicas}' 2>/dev/null); \
	  A_PEND=$$(kubectl get pods -n $(POC_NS) -l app=model-a-decode \
	    --field-selector=status.phase=Pending --no-headers 2>/dev/null \
	    | wc -l | tr -d ' '); \
	  B_PEND=$$(kubectl get pods -n $(POC_NS) -l app=model-b-decode \
	    --field-selector=status.phase=Pending --no-headers 2>/dev/null \
	    | wc -l | tr -d ' '); \
	  echo "  [$$(printf '%3d' $$((i*10)))s]  model-a: $$A_RUN run + $$A_PEND pend (max=$$A_MAX)  |  model-b: $$B_RUN run + $$B_PEND pend"; \
	done
	@echo ""
	@echo "--- Phase 1 snapshot ---"
	@kubectl get hpa -n $(POC_NS) \
	  -o custom-columns='HPA:.metadata.name,MAX:.spec.maxReplicas,CURRENT:.status.currentReplicas,DESIRED:.status.desiredReplicas'
	@echo ""
	@echo "--- Phase 2: Add equal model-b load (starvation should appear) ---"
	@echo "  Starting model-b load job (80 concurrent burst + equal drip)..."
	@kubectl apply -f $(POC_DIR)/starvation-load-b.yaml
	@echo "  Watching starvation develop (90s, 10s intervals)..."
	@echo ""
	@for i in $$(seq 1 9); do \
	  sleep 10; \
	  A_RUN=$$(kubectl get hpa model-a-hpa -n $(POC_NS) \
	    -o jsonpath='{.status.currentReplicas}' 2>/dev/null); \
	  B_RUN=$$(kubectl get hpa model-b-hpa -n $(POC_NS) \
	    -o jsonpath='{.status.currentReplicas}' 2>/dev/null); \
	  A_PEND=$$(kubectl get pods -n $(POC_NS) -l app=model-a-decode \
	    --field-selector=status.phase=Pending --no-headers 2>/dev/null \
	    | wc -l | tr -d ' '); \
	  B_PEND=$$(kubectl get pods -n $(POC_NS) -l app=model-b-decode \
	    --field-selector=status.phase=Pending --no-headers 2>/dev/null \
	    | wc -l | tr -d ' '); \
	  STARVED=""; \
	  if [ "$${B_PEND:-0}" -ge 3 ]; then STARVED=" <-- STARVATION"; fi; \
	  echo "  [$$(printf '%3d' $$((i*10)))s]  model-a: $$A_RUN run + $$A_PEND pend  |  model-b: $$B_RUN run + $$B_PEND pend$$STARVED"; \
	done
	@echo ""
	@echo "================================================================"
	@echo "  STARVATION STATE"
	@echo "================================================================"
	@echo ""
	@kubectl get hpa -n $(POC_NS) \
	  -o custom-columns='HPA:.metadata.name,MAX:.spec.maxReplicas,CURRENT:.status.currentReplicas,DESIRED:.status.desiredReplicas'
	@echo ""
	@echo "  Decode pods:"
	@kubectl get pods -n $(POC_NS) --no-headers 2>/dev/null \
	  | grep "model-[ab]-decode" | sort \
	  | awk '{printf "  %-50s %s\n", $$1, $$3}'
	@echo ""
	@echo "  Scheduler events (FailedScheduling):"
	@kubectl get events -n $(POC_NS) --field-selector=reason=FailedScheduling \
	  --no-headers 2>/dev/null \
	  | grep "model-[ab]-decode" | tail -5 \
	  | awk '{printf "  %s\n", $$0}' \
	  || echo "  (none yet — starvation may still be developing, wait 30s and re-check with poc-status)"
	@echo ""
	@echo "  → Run 'make poc-rebalance' to apply the Coordinator and resolve starvation"
	@echo ""

.PHONY: poc-rebalance
poc-rebalance: ## [POC] Apply Coordinator + GPU quota → watch rebalancing resolve starvation
	@echo ""
	@echo "================================================================"
	@echo "  POC Demo: Coordinator Rebalance"
	@echo "================================================================"
	@echo ""
	@echo "--- Current state (pre-rebalance) ---"
	@kubectl get hpa -n $(POC_NS) \
	  -o custom-columns='HPA:.metadata.name,MAX:.spec.maxReplicas,CURRENT:.status.currentReplicas,DESIRED:.status.desiredReplicas' \
	  2>/dev/null
	@PEND=$$(kubectl get pods -n $(POC_NS) \
	    --field-selector=status.phase=Pending --no-headers 2>/dev/null \
	    | grep "model-[ab]-decode" | wc -l | tr -d ' '); \
	  echo "  Pending model pods: $$PEND"
	@echo ""
	@echo "--- Step 1: Activate Coordinator for these HPAs ---"
	@echo "  Applying rebalance overlay — adds epp-inference-pool annotation to both HPAs"
	@kubectl apply -k $(POC_DIR)/overlays/rebalance
	@echo "  ResourceQuota (still in effect from starvation):"
	@kubectl get resourcequota -n $(POC_NS)
	@echo ""
	@echo "--- Step 2: Coordinator config (should show EXPERIMENTAL_COORDINATOR_ENABLED: \"true\") ---"
	@kubectl get configmap wva-manager-config -n $(POC_WVA_NS) \
	  -o jsonpath='{.data.config\.yaml}' 2>/dev/null \
	  | grep -E "COORDINATOR" | sed 's/^/  /'
	@echo ""
	@echo "--- Watching rebalance (90s, 5s intervals) ---"
	@echo "  Coordinator ticks every 15s — first decision within ~15s"
	@echo ""
	@for i in $$(seq 1 18); do \
	  sleep 5; \
	  A_MAX=$$(kubectl get hpa model-a-hpa -n $(POC_NS) \
	    -o jsonpath='{.spec.maxReplicas}' 2>/dev/null); \
	  B_MAX=$$(kubectl get hpa model-b-hpa -n $(POC_NS) \
	    -o jsonpath='{.spec.maxReplicas}' 2>/dev/null); \
	  A_RUN=$$(kubectl get hpa model-a-hpa -n $(POC_NS) \
	    -o jsonpath='{.status.currentReplicas}' 2>/dev/null); \
	  B_RUN=$$(kubectl get hpa model-b-hpa -n $(POC_NS) \
	    -o jsonpath='{.status.currentReplicas}' 2>/dev/null); \
	  A_PEND=$$(kubectl get pods -n $(POC_NS) -l app=model-a-decode \
	    --field-selector=status.phase=Pending --no-headers 2>/dev/null \
	    | wc -l | tr -d ' '); \
	  B_PEND=$$(kubectl get pods -n $(POC_NS) -l app=model-b-decode \
	    --field-selector=status.phase=Pending --no-headers 2>/dev/null \
	    | wc -l | tr -d ' '); \
	  COORD=$$(kubectl logs -n $(POC_WVA_NS) \
	    -l app.kubernetes.io/name=workload-variant-autoscaler \
	    --tail=5 2>/dev/null \
	    | grep -iE "Setting maxReplicas|contention" | tail -1); \
	  echo "  [$$(printf '%3d' $$((i*5)))s]  model-a: max=$$A_MAX run=$$A_RUN pend=$$A_PEND  |  model-b: max=$$B_MAX run=$$B_RUN pend=$$B_PEND"; \
	  if [ -n "$$COORD" ]; then echo "         ↳ $$COORD"; fi; \
	done
	@echo ""
	@echo "================================================================"
	@echo "  REBALANCE RESULT"
	@echo "================================================================"
	@echo ""
	@kubectl get hpa -n $(POC_NS) \
	  -o custom-columns='HPA:.metadata.name,MAX:.spec.maxReplicas,CURRENT:.status.currentReplicas,DESIRED:.status.desiredReplicas'
	@echo ""
	@echo "  ResourceQuota:"
	@kubectl get resourcequota -n $(POC_NS)
	@echo ""
	@echo "  Decode pods:"
	@kubectl get pods -n $(POC_NS) --no-headers 2>/dev/null \
	  | grep "model-[ab]-decode" | sort \
	  | awk '{printf "  %-50s %s\n", $$1, $$3}'
	@echo ""
	@echo "  Coordinator decisions (last 10 entries):"
	@kubectl logs -n $(POC_WVA_NS) \
	  -l app.kubernetes.io/name=workload-variant-autoscaler \
	  --tail=60 2>/dev/null \
	  | grep -iE "gpu-rebalance|Setting maxReplicas|contention" \
	  | tail -10 | sed 's/^/  /' \
	  || echo "  (none)"
	@echo ""
