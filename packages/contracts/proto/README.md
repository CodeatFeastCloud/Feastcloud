# Edge synchronization protocol

`feastcloud.sync.v1` is the normative Phase 0 edge/cloud contract. Protobuf field names use snake case; standard Protobuf JSON mapping produces the public camelCase names. Receivers must accept duplicate batches and operations, and must return one terminal or retry result per operation.

The current and immediately previous released protocol versions remain supported. Generated code is not committed until reproducible Buf configuration and compatibility checks are enabled.

