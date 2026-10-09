// Package pb holds generated Go protobuf/gRPC stubs for ifx-agent's wire
// contract, defined in the informix-odbc repo's agent/ifx_agent.proto:
// https://github.com/simichanga/informix-odbc/blob/master/agent/ifx_agent.proto
//
// Committed directly (not gitignored, not regenerated at build time) so
// BOP's own build stays pure Go with no protoc dependency - this driver
// ships as a separately-built binary BOP spawns as a subprocess, not
// something BOP compiles from source.
//
// Regenerate after a change to the upstream .proto with:
//
//	protoc --go_out=internal/plugin/informix/ifxagentpb --go_opt=paths=source_relative \
//	       --go-grpc_out=internal/plugin/informix/ifxagentpb --go-grpc_opt=paths=source_relative \
//	       -I <path-to-informix-odbc>/agent <path-to-informix-odbc>/agent/ifx_agent.proto
package pb
