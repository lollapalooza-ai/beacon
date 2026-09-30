# Beacon — Autonomous Cloud Compute Procurement Agent

Beacon autonomously discovers, bids on, provisions, and decommissions cloud spot instances based on natural language workload intents. It uses LLMs to parse user intents and evaluate spot pricing options, driving a complete lifecycle state machine from discovery to teardown.

## Features (Community Edition MVP)

- **Model-Agnostic LLM Layer** — Ollama, OpenAI, Anthropic, and Gemini support behind a single `Provider` interface
- **AWS EC2 Spot Integration** — Real spot pricing queries and instance provisioning via AWS SDK v2
- **Autonomous Lifecycle** — Full state machine: Discovery → Bidding → Authorization → Provisioning → Monitoring → Decommission
- **Crash Recovery** — SQLite state persistence detects and cleans up orphaned compute on restart
- **Safety Guards** — Circuit breakers, rate limiters, retry limits, and budget enforcement prevent runaway costs
- **Structured Decision Logging** — zerolog JSON audit trail of every bidding evaluation and state transition

## Quick Start

```bash
# Build
go build -o beacon ./cmd/beacon/

# Run with Ollama (local LLM)
export BEACON_LLM_PROVIDER=ollama
export BEACON_LLM_MODEL=llama3
beacon run --intent "Train ResNet on 4xA100 under $50"

# Run with OpenAI
export BEACON_LLM_PROVIDER=openai
export BEACON_LLM_API_KEY=sk-...
export BEACON_LLM_MODEL=gpt-4o
beacon run --intent "Run inference on T4 GPU in us-west-2" --budget 25

# Check active workloads
beacon status

# Recover orphaned workloads after a crash
beacon recover
```

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|---|---|---|
| `BEACON_LLM_PROVIDER` | `ollama` | LLM provider: `ollama`, `openai`, `anthropic`, `gemini` |
| `BEACON_LLM_MODEL` | `llama3` | Model identifier |
| `BEACON_LLM_ENDPOINT` | `http://localhost:11434` | LLM API endpoint (required for Ollama) |
| `BEACON_LLM_API_KEY` | — | API key (required for hosted providers) |
| `BEACON_AWS_REGION` | `us-east-1` | Default AWS region |
| `BEACON_AWS_PROFILE` | — | AWS credentials profile |
| `BEACON_MAX_BUDGET` | `100.0` | Global maximum budget per workload (USD) |
| `BEACON_DB_PATH` | `beacon.db` | SQLite database path |
| `BEACON_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `BEACON_LOG_FORMAT` | `console` | Log format: `json`, `console` |
| `BEACON_MAX_PROVISION_RETRIES` | `3` | Max provisioning retry attempts |
| `BEACON_MAX_DISCOVERY_RETRIES` | `5` | Max discovery retry attempts |
| `BEACON_RATE_LIMIT` | `10.0` | Cloud API rate limit (requests/sec) |

## Project Structure

```
beacon/
├── cmd/beacon/              # CLI entry point (cobra)
├── pkg/
│   ├── orchestrator/        # Core state machine, intent parsing, bidding
│   ├── llm/                 # Model-agnostic LLM abstraction (4 providers)
│   ├── cloud/               # Cloud adapter interface + AWS EC2 Spot
│   │   └── aws/             # AWS EC2 Spot adapter
│   ├── payment/             # Payment gateway interface + stub
│   ├── state/               # SQLite state persistence + crash recovery
│   └── config/              # Environment-based configuration
├── internal/testing/        # Mock cloud server + test harnesses
├── design/                  # Design documents
└── docs/                    # ADRs and API specs
```

## Architecture

```
User Intent (CLI) → Orchestrator State Machine
                         │
    ┌────────────────────┼────────────────────┐
    ▼                    ▼                    ▼
LLM Provider       Cloud Adapter       Payment Gateway
(parse intent,      (spot pricing,       (budget auth,
 rank bids)          provision,           token mgmt)
                     terminate)
    │                    │                    │
    ▼                    ▼                    ▼
Ollama/OpenAI/      AWS EC2 Spot         Stub Gateway
Anthropic/Gemini    (+ circuit breaker)  (MVP)
```

## License

TBD
