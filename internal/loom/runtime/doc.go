// Package runtime owns execution contracts, capability checks and the ordered,
// locked adapter registry. It has no globals or concrete adapters. Message,
// per-turn capabilities, callback and quota snapshot types are supplied by the
// caller so this leaf package does not depend on Loom's discussion, tool or usage
// implementation. Loom's compatibility aliases bind the existing types without
// converting payloads or changing their JSON shapes.
package runtime
