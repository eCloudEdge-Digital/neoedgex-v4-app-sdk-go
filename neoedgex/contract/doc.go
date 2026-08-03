// Package contract holds the shared type definitions of the NeoFlow wire
// pipeline: node configuration, port schemas, the message accessors and the
// data-type conversion rules.
//
// The neoedgex package aliases the few types an ordinary handler touches (Node,
// Message, Logger, ErrorCode and the error-code constants). The schema-typing
// identifiers — DataType, the Type* constants and PortFieldSchema — have no
// alias, so an application that inspects a node's port schema (for example
// NodeConfig().Data.Outputs[handle][i].Type) imports this package directly.
package contract
