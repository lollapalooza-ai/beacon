Assuming the initial target workloads involve standard containerized microservices and ML training jobs across AWS and GCP, with an emphasis on strict budget limits per job. To ensure high-performance concurrent API polling and seamless AI integration, the architecture assumes a backend written in Go, alongside Python for the AI abstraction layer, natively supporting local inference tools like Ollama for testing.

### Edition Segregation: Community vs. Enterprise

The architecture must strictly separate the open-source Community Edition from the commercial Enterprise Edition.

| Feature Category | Open-Source Core (Community Edition) | B2B Premium (Enterprise Edition) |
| --- | --- | --- |
| **Discovery & Bidding** | Basic API querying across cloud providers for spot pricing, CPU/GPU availability, and latency metrics.

 | Predictive bidding algorithms, historical spot pricing analytics, and reserved-instance integration.

 |
| **Provisioning** | Scripts and standard APIs to spin up instances, deploy workloads, and autonomously decommission compute.

 | Multi-cluster orchestration, SLA-backed uptime guarantees, and enterprise compliance reporting.

 |
| **Security & Access** | Basic API key management for cloud environments.

 | Role-Based Access Control (RBAC), SSO, and audit logging for large engineering teams.

 |
| **Financial Controls** | Basic threshold alerts for cloud spending. Secure agentic payment protocols using tokenized credentials as the core transaction mechanism.

 | Strict spending constraints enforced at the payment network level to prevent runaway costs from AI hallucination loops.

 |

### System Architecture: Beacon Community Edition

The Community Edition handles the end-to-end flow: Trigger & Intent, Discovery & Bidding, Secure Authorization, and Provisioning & Rollback.

1. **Agent Orchestrator (Go):** The core control loop. It receives workload intents, translates them into compute requirements, and manages the state machine.
2. **LLM Abstraction Layer (Python/Go):** Standardizes prompts and parses JSON outputs. It interfaces with external APIs (OpenAI, Anthropic) and local instances (Ollama) to keep the engine model-agnostic.
3. **Cloud API Adapters:** Pluggable modules for AWS EC2 Spot, GCP Spot VMs, and specialized GPU clouds. They query machine-readable APIs to compare spot pricing and availability.


4. **Payment & Tokenization Gateway:** Implements an agentic payment protocol (e.g., Agentic Commerce Protocol or Stripe shared payment tokens). It requests single-use, tightly scoped cryptographic tokens to authorize transactions without holding raw corporate credit card data.
5. **Provisioning & Lifecycle Manager:** Executes the deployment, monitors health, and autonomously decommissions compute to prevent runaway costs.



---

### Implementation Prompt for Senior Engineer

**Objective:** Implement the MVP of Beacon Community Edition. The system must autonomously procure, provision, and decommission cloud compute based on workload intents.

**1. AI Model Agnosticism**

* Implement a `Provider` interface that normalizes interactions across different LLMs.
* Ensure the abstraction supports both hosted models (e.g., Claude 3.5 Sonnet, GPT-4o) and local execution (e.g., Llama 3 via Ollama) to allow developers to run the agentic loop locally on standard hardware like an M4 chip.
* The LLM should strictly output structured JSON (enforced via schema or tree-sitter grammars) to drive the state machine reliably.

**2. Core Architecture & Concurrency**

* Build the core orchestrator in Go to leverage goroutines for concurrent API querying across multiple cloud providers.
* Implement circuit breakers and rate limiters on the Cloud API Adapters to handle provider throttling gracefully.
* The system must act continuously: discover the optimal cluster, authorize payment, provision the workload, and decommission upon completion.



**3. Project Structure**
Adopt a standard, modular layout to encourage open-source contributions:

* `cmd/beacon/`: Main application entry points.
* `pkg/orchestrator/`: Core state machine and intent parsing.
* `pkg/llm/`: The model-agnostic abstraction layer and prompt templates.
* `pkg/cloud/`: Interfaces and adapters for AWS, GCP, etc.
* `pkg/payment/`: Tokenization logic and secure credential handling.
* `internal/testing/`: Mocks and test harnesses.
* `docs/`: Architecture Decision Records (ADRs) and API specifications.

**4. Testing Strategy (Mandatory)**

* **Unit Tests:** Minimum 80% coverage on the `orchestrator` and `cloud` packages.
* **Mock Cloud APIs:** Implement an in-memory test server that simulates cloud provider responses, spot price fluctuations, and API failures.
* **Hallucination Safeguards:** Write specific integration tests simulating an AI agent stuck in a retry loop. Verify that the orchestrator enforces hard limits on provisioning API calls to prevent unwanted compute spikes.



**5. Secure Payment Integration**

* Do not store or transmit raw credit card data.
* Integrate a standard agentic payment protocol (e.g., ACP). The agent must request a tokenized credential scoped strictly to the approved budget and specific merchant (cloud provider).
* Ensure the authorization flow requires explicit budget confirmation before the token is minted and passed to the provisioning layer.

**6. Critical Missing Areas to Address**

* **State Persistence & Crash Recovery:** If the agent orchestrator crashes after provisioning but before decommissioning, you risk orphaned compute. Implement a persistent state store (e.g., SQLite for Community Edition) to track active workloads and resume teardown upon restart.
* **Telemetry & Auditability:** The agent's decision-making process (why it chose GCP over AWS for a specific job) must be logged comprehensively. Emit structured logs for every bidding evaluation and payment token request.

Before we finalize the API payload structures, what specific latency metrics (e.g., network latency to specific regions vs. disk I/O) are most critical for your initial target workloads?