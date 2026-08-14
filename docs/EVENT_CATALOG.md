# EVENT_CATALOG.md

Canonical map of every event topic in the platform, its producer(s), consumer(s),
and orphan status. Generated from the assurance audit (stage1 §5, R4-51) and kept
as the explicit contract so orphaned or renamed topics are visible instead of
silently evaporating.

**Name-mismatch fix (R4-51):** the external-device event stream was split between
`inec.bvas.device-events.v1` (Kafka/Fluvio topic constants in
`inec-go-backend/external_integration_delivery.go`) and
`inec.bvas.device-event.v1` (singular; outbox `event_type` written by
`device_gateway.go` and subscribed by `services/lakehouse-analytics`). All four
now use the canonical plural name **`inec.bvas.device-events.v1`**.

| Topic | Producer (file) | Consumer | Status |
|---|---|---|---|
| inec.results.submitted | go-backend mw_kafka.go (TopicResultSubmitted) | rust-hot-path (pipeline.rs:59) — only when built with the `kafka` feature | **Effectively orphan** (default build refuses) |
| inec.ballots.cast | go-throughput-engine/kafka_batch.go:198; dapr_bulk_processor.py:123 | rust-hot-path (pipeline.rs:59); python-pipeline-optimizer consumer commented out (kafka_arrow_consumer.py:60-61) | Conditional orphan |
| inec.incidents.reported | go-backend mw_kafka.go | rust-hot-path | Conditional orphan |
| inec.results.validated | go-backend mw_kafka.go | fluvio-stream ensures topic exists; no consumer | **Orphan** |
| inec.results.finalized | go-backend mw_kafka.go | fluvio-stream ensures topic exists; no consumer | **Orphan** |
| inec.results.disputed | go-backend mw_kafka.go | fluvio-stream ensures topic exists; no consumer | **Orphan** |
| inec.collation.updates | go-throughput-engine/kafka_batch.go:204; dapr_bulk_processor.py:126 | none | **Orphan** |
| inec.audit.log | go-backend mw_kafka.go | none | **Orphan** |
| inec.bvas.accreditation | go-backend bvas.go | none found | **Orphan** |
| inec.ingestion.result | go-backend ingestion.go | none found | **Orphan** |
| inec.ingestion.accreditation | go-backend ingestion.go | none found | **Orphan** |
| inec.observer.alert | go-backend observer_monitoring.go | none | **Orphan** |
| inec.observer.checkin | go-backend observer_monitoring.go | none | **Orphan** |
| inec.observer.report | go-backend observer_monitoring.go | none | **Orphan** |
| **inec.bvas.device-events.v1** (Kafka/Fluvio/Dapr `pubsub`) | go-backend device_gateway.go (outbox `event_type`) + external_integration_delivery.go (delivers to kafka/dapr/fluvio sinks) | lakehouse-analytics `/dapr/events/external-device` (main.py; `DAPR_EXTERNAL_DEVICE_TOPIC`, default `inec.bvas.device-events.v1`) | **Paired** ✔ (fixed in R4-51; was a singular/plural name mismatch → silent drop) |
| inec.fluvio.ingest | go-backend mw_kafka.go | none | Orphan |
| gotv.outreach.queue | gotv-svc dispatch_v2.go:28 | gotv-svc KafkaConsumer (main.go:193, v2_constructors.go:188-200) | **Paired** ✔ |
| gotv.outreach.dlq | gotv-svc dispatch_v2.go:29 | gotv-svc KafkaConsumer | **Paired** ✔ |
| gotv.audit | gotv-svc middleware.go + handlers | none | Orphan |
| gotv.campaigns | gotv-svc handlers | none | Orphan |
| gotv.canvass | gotv-svc handlers | none | Orphan |
| gotv.pledges | gotv-svc handlers | none | Orphan |
| gotv.rides | gotv-svc handlers; gotv-engine main.rs:451 | none | Orphan |
| gotv.volunteers | gotv-svc handlers | none | Orphan |
| gotv-field-reports | gotv-svc handlers | none | Orphan |
| gotv-voice-calls | gotv-svc handlers | none | Orphan |
| gotv-tasks | gotv-svc handlers | none | Orphan |
| gotv.analytics | gotv-analytics main.py:474 | none | Orphan |
| gotv-anomalies | gotv-analytics main.py:500 | none | Orphan |
| gotv-ride-matches (Fluvio) | gotv-engine main.rs:452 | none | Orphan |
| inec.stream.results (Fluvio) | rust-hot-path pipeline.rs:77 | none | Orphan |
| inec.stream.ballots (Fluvio) | rust-hot-path pipeline.rs:77 | none | Orphan |
| Dapr pubsub component `pubsub` (Redis) | dapr-components/pubsub.yaml | lakehouse-analytics `/dapr/subscribe` | Infrastructure pairing ✔ |

## Notes

- "Orphan" means no in-repo consumer exists. External/offline consumers (BI
  exports, manual audit pulls) are not visible to this catalog; if one exists,
  register the topic here with its consumer.
- Known related defect: Temporal workflows are started by
  `external_integration_delivery.go` (task queue `external-integration`) but no
  worker is deployed in-repo (D1 EV-3) — starts will queue indefinitely.
