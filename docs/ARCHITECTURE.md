# INEC Platform Architecture
The platform consists of a Go backend, React frontend, Rust biometric service, and Python ML service.
It uses PostgreSQL, TigerBeetle, Redis, Temporal, and Fluvio for persistence and messaging.
Authentication is handled by Keycloak, authorization by Permify, and API gateway by APISIX.
